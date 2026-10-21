package webserver

import (
	"net/http"
	"strings"
	"testing"

	"rasbeacon/internal/api"
)

// Every answer, the pages and the API alike, carries the browser
// protections: scripts only from rbweb itself, never inside another site.
func TestSecurityHeaders_AreOnEveryAnswer(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	for _, path := range []string{"/", "/api/v1/setup", "/api/v1/no-such-route", "/api/v1/status"} {
		rec := request(s, "GET", "http://10.87.0.1"+path, "", nil, nil)
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("GET %s: Content-Security-Policy = %q", path, csp)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("GET %s: headers = %v", path, rec.Header())
		}
	}
	// Refusals too: the "wrong name" answer is a page another site could frame.
	rec := request(s, "GET", "http://evil.example/", "", nil, nil)
	if rec.Code != http.StatusMisdirectedRequest || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("refusal = %d %v", rec.Code, rec.Header())
	}
}
