package webserver

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// rbdOnlyRoutes are rbd's routes that rbweb reaches some other way (its own
// handlers, before login, or streamed) rather than through forwardedRoutes.
var rbdOnlyRoutes = []string{
	"GET /state", "POST /claim", "POST /verify-password",
	"POST /remembered-logins", "POST /remembered-logins/check", "POST /remembered-logins/forget",
	"GET /status", "POST /speedtest", "POST /system/update", "POST /system/factory-reset",
	"GET /portal", "GET /portal/browser/frame", "POST /portal/browser/input",
	"POST /portal/browser/start", "POST /portal/browser/stop",
	"POST /portal/start", "POST /portal/stop", "POST /portal/extend", "POST /portal/check",
	// Streamed or slow: rbweb has its own handlers (see Handler).
	"POST /plugins/upload", "POST /plugins/catalog", "POST /plugins/catalog/download", "POST /plugins/update",
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
