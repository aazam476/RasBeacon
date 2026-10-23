package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// checksFor points both checks at a test server.
func checksFor(server *httptest.Server) []Check {
	return []Check{
		{URL: server.URL + "/generate_204", WantStatus: http.StatusNoContent},
		{URL: server.URL + "/hotspot-detect.html", WantStatus: http.StatusOK, WantBodyContains: "Success"},
	}
}

func TestProbe(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantState State
		wantLogin string
	}{
		{"free internet", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/generate_204" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Write([]byte("<HTML><BODY>Success</BODY></HTML>"))
		}, Open, ""},
		{"redirect to a login page", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login?from=hotel", http.StatusFound)
		}, Portal, "/login?from=hotel"},
		// A Location that isn't a web address would run as script when the
		// UI opens it, so the check's own address is used instead.
		{"script in the Location", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "javascript:alert(1)")
			w.WriteHeader(http.StatusFound)
		}, Portal, "/generate_204"},
		{"login page served in place", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>Welcome to Hotel Wi-Fi. Please accept the terms.</html>"))
		}, Portal, "/generate_204"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			got := Probe(context.Background(), server.Client(), checksFor(server))
			if got.State != tt.wantState {
				t.Fatalf("state = %s, want %s", got.State, tt.wantState)
			}
			if tt.wantLogin != "" && got.LoginURL != server.URL+tt.wantLogin {
				t.Errorf("login URL = %s, want %s", got.LoginURL, server.URL+tt.wantLogin)
			}
		})
	}
}

func TestProbe_NoAnswerIsOffline(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	checks := checksFor(server)
	server.Close() // nothing listens any more
	if got := Probe(context.Background(), http.DefaultClient, checks); got.State != Offline {
		t.Errorf("state = %s, want offline", got.State)
	}
}
