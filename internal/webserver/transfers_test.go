package webserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Long transfers keep going as long as data flows (transfers.go), even
// past the server's own time limits, and end once they stall.

// shortTransfers makes the idle limit short for one test.
func shortTransfers(t *testing.T) {
	old := transferIdleTime
	transferIdleTime = 300 * time.Millisecond
	t.Cleanup(func() { transferIdleTime = old })
}

// slowBody sends n chunks, one every gap.
func slowBody(n int, gap time.Duration) io.Reader {
	reader, writer := io.Pipe()
	go func() {
		for range n {
			time.Sleep(gap)
			if _, err := writer.Write([]byte("chunk\n")); err != nil {
				return
			}
		}
		writer.Close()
	}()
	return reader
}

// The server below allows 150 ms per request; the transfers take a second.
func newSlowServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	server := httptest.NewUnstartedServer(handler)
	server.Config.ReadTimeout = 150 * time.Millisecond
	server.Config.WriteTimeout = 150 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestSlidingReader_AnUploadThatKeepsMovingIsntCut(t *testing.T) {
	shortTransfers(t)
	got := make(chan string, 1)
	server := newSlowServer(t, func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(transferIdleTime))
		data, err := io.ReadAll(slidingReader{r: r.Body, rc: rc})
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		got <- string(data)
	})

	// Ten chunks, 100 ms apart: a second in all, but never 300 ms idle.
	resp, err := http.Post(server.URL, "application/octet-stream", slowBody(10, 100*time.Millisecond))
	if err == nil {
		resp.Body.Close()
	}
	if body := <-got; strings.Count(body, "chunk") != 10 {
		t.Errorf("the server got %q, want all 10 chunks", body)
	}

	// A stall longer than the idle limit ends it.
	resp, err = http.Post(server.URL, "application/octet-stream", slowBody(2, 600*time.Millisecond))
	if err == nil {
		resp.Body.Close()
	}
	if body := <-got; !strings.HasPrefix(body, "error:") {
		t.Errorf("a stalled upload should end, the server got %q", body)
	}
}

func TestSlidingWriter_ADownloadThatKeepsMovingIsntCut(t *testing.T) {
	shortTransfers(t)
	server := newSlowServer(t, func(w http.ResponseWriter, r *http.Request) {
		out := slidingWriter{w: w, rc: http.NewResponseController(w)}
		for range 10 {
			time.Sleep(100 * time.Millisecond)
			_, _ = out.Write([]byte("chunk\n"))
			_ = http.NewResponseController(w).Flush()
		}
	})
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Count(string(body), "chunk") != 10 {
		t.Errorf("the download got %q, want all 10 chunks", body)
	}
}
