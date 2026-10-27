package doh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew_RefusesBadOptions(t *testing.T) {
	for name, o := range map[string]Options{
		"no servers":          {},
		"plain http":          {Servers: []string{"http://dns.example/dns-query"}},
		"no host":             {Servers: []string{"https:///dns-query"}},
		"bootstrap not an IP": {Servers: []string{"https://dns.example/dns-query"}, Bootstrap: []string{"dns.google"}},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	f, err := New(Options{Servers: []string{"https://dns.example/dns-query"}, Bootstrap: []string{"9.9.9.9"}})
	if err != nil || f.timeout != 4*time.Second || f.lookup == nil {
		t.Errorf("defaults: %v, timeout %v", err, f.timeout)
	}
}

// Answers that aren't DNS are refused, and the device gets SERVFAIL
// rather than garbage.
func TestAsk_RefusesAnswersThatArentDNS(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"an error page": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "busy", http.StatusServiceUnavailable)
		},
		"a web page": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		},
		"too short": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Write([]byte{1, 2, 3})
		},
	} {
		srv := httptest.NewTLSServer(handler)
		f, err := New(Options{Servers: []string{srv.URL}, Transport: srv.Client().Transport, Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		q := question(7, "example.com", 0)
		if _, err := f.ask(context.Background(), f.servers[0], q); err == nil {
			t.Errorf("%s: accepted", name)
		}
		answer := f.Exchange(context.Background(), q)
		if len(answer) < headerLen || answer[3]&0x0f != 2 { // RCODE 2 = SERVFAIL
			t.Errorf("%s: answer %v, want SERVFAIL", name, answer)
		}
		srv.Close()
	}
}

// A message shorter than a DNS header isn't a question: no answer.
func TestExchange_IgnoresScraps(t *testing.T) {
	f, _ := New(Options{Servers: []string{"https://dns.example/dns-query"}})
	if answer := f.Exchange(context.Background(), []byte{1, 2}); answer != nil {
		t.Errorf("answered %v", answer)
	}
}

// Without bootstrap servers or a known address, a server's name can't be
// looked up, and the error says so.
func TestResolve_WithoutBootstrapServers(t *testing.T) {
	f, _ := New(Options{Servers: []string{"https://dns.example/dns-query"}})
	if _, err := f.resolve(context.Background(), "dns.example"); err == nil || !strings.Contains(err.Error(), "no bootstrap DNS servers") {
		t.Errorf("got %v", err)
	}
	// dial passes the same error on, and refuses an address without a port.
	if _, err := f.dial(context.Background(), "tcp", "dns.example:443"); err == nil {
		t.Error("dial should fail without a way to look the name up")
	}
	if _, err := f.dial(context.Background(), "tcp", "no-port"); err == nil {
		t.Error("an address without a port can't be dialled")
	}
}
