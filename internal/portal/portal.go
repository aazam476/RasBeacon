// Package portal detects "captive portals": the login or terms-of-use page
// that hotel, airport and café networks show before letting anyone online.
//
// It works like phones do. It asks a few well-known addresses that always
// give a fixed, known answer (for example an empty "204 No Content"). If
// the answer is different (usually a redirect to a login page), the network
// is holding traffic back until someone logs in.
package portal

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// Check is one well-known address and the answer it gives on a free
// network.
type Check struct {
	URL string
	// WantStatus is the expected HTTP status code.
	WantStatus int
	// WantBodyContains, if set, must appear in the body.
	WantBodyContains string
}

// Checks are the addresses RasBeacon asks, the same ones Android and Apple
// devices use.
var Checks = []Check{
	{URL: "http://connectivitycheck.gstatic.com/generate_204", WantStatus: http.StatusNoContent},
	{URL: "http://captive.apple.com/hotspot-detect.html", WantStatus: http.StatusOK, WantBodyContains: "Success"},
}

// State is what a probe found.
type State string

const (
	// Open: the internet works.
	Open State = "open"
	// Portal: a login page is in the way.
	Portal State = "portal"
	// Offline: no answer at all (no internet, or everything is blocked).
	Offline State = "offline"
)

// Result is what Probe found.
type Result struct {
	State State
	// LoginURL is where the network sent us (the login page), when known.
	LoginURL string
}

// Probe asks every check and combines the answers: any sign of a login
// page wins, then any normal answer, otherwise offline.
func Probe(ctx context.Context, client *http.Client, checks []Check) Result {
	// Don't follow redirects: a redirect IS the sign of a login page.
	noRedirects := *client
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	result := Result{State: Offline}
	for _, check := range checks {
		r := probeOne(ctx, &noRedirects, check)
		switch {
		case r.State == Portal:
			return r
		case r.State == Open:
			result = r
		}
	}
	return result
}

// probeOne asks one check.
func probeOne(ctx context.Context, client *http.Client, check Check) Result {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, check.URL, nil)
	if err != nil {
		return Result{State: Offline}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{State: Offline}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode == check.WantStatus &&
		(check.WantBodyContains == "" || strings.Contains(string(body), check.WantBodyContains)) {
		return Result{State: Open}
	}
	// Anything else means someone answered in the real server's place.
	loginURL := check.URL
	if location := resp.Header.Get("Location"); location != "" {
		if u, err := resp.Request.URL.Parse(location); err == nil {
			loginURL = u.String()
		}
	}
	return Result{State: Portal, LoginURL: loginURL}
}

// ProbeHostnames are the names phones and computers use for their own
// checks. While RasBeacon shows its "login needed" notice, its DNS server
// answers these with its own address, so the device's "Log in to network"
// sheet opens with RasBeacon's notice page.
var ProbeHostnames = []string{
	"captive.apple.com",
	"www.apple.com",
	"connectivitycheck.gstatic.com",
	"connectivitycheck.android.com",
	"clients3.google.com",
	"www.google.com",
	"detectportal.firefox.com",
	"www.msftconnecttest.com",
	"www.msftncsi.com",
	"nmcheck.gnome.org",
}
