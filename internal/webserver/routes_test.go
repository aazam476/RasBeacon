package webserver

import (
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"rasbeacon/internal/api"
)

// rbdOnlyRoutes are rbd's routes that rbweb reaches some other way (its own
// handlers, before login, or streamed) rather than through forwardedRoutes.
var rbdOnlyRoutes = []string{
	"GET /state", "POST /claim", "POST /verify-password",
	"POST /remembered-logins", "POST /remembered-logins/check", "POST /remembered-logins/forget",
	"GET /status", "POST /speedtest", "POST /system/update", "POST /system/factory-reset", "POST /system/admin-password",
	"GET /portal", "GET /portal/browser/frame", "POST /portal/browser/input",
	"POST /portal/browser/start", "POST /portal/browser/stop",
	"POST /portal/start", "POST /portal/stop", "POST /portal/extend", "POST /portal/check",
	// Streamed or slow: rbweb has its own handlers (see Handler).
	"POST /plugins/upload", "POST /plugins/catalog", "POST /plugins/catalog/download", "POST /plugins/update",
	"POST /plugins/start", "POST /plugins/stop", "POST /plugins/remove", "PUT /plugins/settings",
	"GET /system/backup/{token}", "POST /system/restore/upload", "POST /system/restore",
	// The welcome page's own server (see WelcomeHandler).
	"GET /welcome/page", "POST /welcome/accept",
}

// Every route rbd offers must be reachable through rbweb: a settings page
// whose route rbweb doesn't forward fails on the device with "no such API
// route" while working in the demo (this happened to the swap settings).
func TestEveryRbdRouteIsForwarded(t *testing.T) {
	source, err := os.ReadFile("../agent/server.go")
	if err != nil {
		t.Fatal(err)
	}
	forwarded := map[string]bool{}
	for public, rbdPath := range forwardedRoutes {
		method, _, _ := strings.Cut(public, " ")
		forwarded[method+" "+rbdPath] = true
	}
	// Routes registered directly, and the ones built from a table.
	pattern := regexp.MustCompile(`"((?:GET|PUT|POST|DELETE) /[^"]*)"`)
	for _, m := range pattern.FindAllStringSubmatch(string(source), -1) {
		route := m[1]
		if slices.Contains(rbdOnlyRoutes, route) {
			continue
		}
		if !forwarded[route] {
			t.Errorf("rbd route %q is not forwarded by rbweb (add it to forwardedRoutes, or to rbdOnlyRoutes if rbweb handles it itself)", route)
		}
	}
}

// documentedRoutes reads every route docs/api.md lists, like
// "POST /api/v1/system/hostname". The web UI's request tests check their
// requests against the same list (web/test/requests/helpers.ts), so the
// document is the contract between the two.
func documentedRoutes(t *testing.T) []string {
	t.Helper()
	doc, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile("`((?:GET|PUT|POST|DELETE) /api/v1/[^`\\s]*)`")
	var routes []string
	for _, m := range pattern.FindAllStringSubmatch(string(doc), -1) {
		route, _, _ := strings.Cut(m[1], "?") // "?after=<seq>" describes a parameter
		if !slices.Contains(routes, route) {
			routes = append(routes, route)
		}
	}
	if len(routes) < 50 {
		t.Fatalf("found only %d routes in docs/api.md; did its format change?", len(routes))
	}
	return routes
}

// Every documented route must exist in rbweb: answered by a handler (perhaps
// "log in first"), never "no such API route" or "method not allowed".
func TestEveryDocumentedRouteExists(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	for _, route := range documentedRoutes(t) {
		method, path, _ := strings.Cut(route, " ")
		path = strings.ReplaceAll(path, "{token}", "abc")
		rec := request(s, method, "http://10.87.0.1"+path, "{}", map[string]string{"Content-Type": "application/json"}, nil)
		if rec.Code == http.StatusMethodNotAllowed || strings.Contains(rec.Body.String(), "no such API route") {
			t.Errorf("%s is in docs/api.md but rbweb doesn't serve it (%d)", route, rec.Code)
		}
	}
}

// Every route rbweb forwards to rbd must be documented.
func TestEveryForwardedRouteIsDocumented(t *testing.T) {
	documented := documentedRoutes(t)
	for route := range forwardedRoutes {
		if !slices.Contains(documented, route) {
			t.Errorf("%s is served by rbweb but missing from docs/api.md", route)
		}
	}
}

// publicRoute reports whether a route may be used without logging in:
// first-time setup, logging in itself, and the hotel login (whose notice
// opens in a phone's "Log in to network" sheet, before any login). See
// the "Public" part of Handler.
func publicRoute(route string) bool {
	method, path, _ := strings.Cut(route, " ")
	switch {
	case path == "/api/v1/setup", path == "/api/v1/setup/claim", path == "/api/v1/session", path == "/api/v1/captive":
		return true
	case path == "/api/v1/portal", strings.HasPrefix(path, "/api/v1/portal/"):
		return method == http.MethodGet || method == http.MethodPost
	}
	return false
}

// Every other documented route refuses a request without a login, so a
// new route can't be added without it by mistake.
func TestEveryOtherRouteNeedsALogin(t *testing.T) {
	s, rbd := newTestServer(api.ModeNormal)
	checked := 0
	for _, route := range documentedRoutes(t) {
		if publicRoute(route) {
			continue
		}
		method, path, _ := strings.Cut(route, " ")
		path = strings.ReplaceAll(path, "{token}", "abc")
		rbd.lastForwarded = ""
		rec := request(s, method, "http://10.87.0.1"+path, "{}", map[string]string{"Content-Type": "application/json"}, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a login answered %d, want 401", route, rec.Code)
		}
		if rbd.lastForwarded != "" {
			t.Errorf("%s without a login reached rbd (%s)", route, rbd.lastForwarded)
		}
		checked++
	}
	if checked < 50 {
		t.Errorf("only %d routes checked; is docs/api.md complete?", checked)
	}
}
