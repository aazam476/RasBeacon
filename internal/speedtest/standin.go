package speedtest

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// StandIn is a small copy of speed.cloudflare.com's test service, for
// tests and the simulated device (which must not contact real servers).
// It sends and receives at about RateMbps, so the test ends as quickly as
// against a real line of that speed.
type StandIn struct {
	RateMbps float64
	// ResultsLogged counts the results the library sent to /__results.
	ResultsLogged atomic.Int32
}

// maxStandInBytes limits one download from the stand-in.
const maxStandInBytes = 300_000_000

// Handler serves the routes the speed test uses.
func (s *StandIn) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cdn-cgi/trace", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fl=1\nh=speed.example\nvisit_scheme=http\ncolo=DEV\nloc=XX\n")
	})
	mux.HandleFunc("GET /locations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"iata":"DEV","city":"Simulated city","region":"Nowhere","cca2":"XX"}]`)
	})
	mux.HandleFunc("GET /__down", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
		n = max(0, min(n, maxStandInBytes))
		w.Header().Set("Content-Length", strconv.Itoa(n))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.Copy(w, s.limit(io.LimitReader(zeros{}, int64(n))))
	})
	mux.HandleFunc("POST /__up", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, s.limit(http.MaxBytesReader(w, r.Body, maxStandInBytes)))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /__results", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20))
		s.ResultsLogged.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"requestId":"stand-in"}`)
	})
	return mux
}

// limit slows a reader down to RateMbps.
func (s *StandIn) limit(r io.Reader) io.Reader {
	if s.RateMbps <= 0 {
		return r
	}
	return &slowReader{r: r, bytesPerSecond: s.RateMbps * 1e6 / 8, start: time.Now()}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type slowReader struct {
	r              io.Reader
	bytesPerSecond float64
	start          time.Time
	done           float64
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(p) > 64*1024 {
		p = p[:64*1024]
	}
	n, err := s.r.Read(p)
	s.done += float64(n)
	if ahead := time.Duration(s.done/s.bytesPerSecond*float64(time.Second)) - time.Since(s.start); ahead > 0 {
		time.Sleep(ahead)
	}
	return n, err
}
