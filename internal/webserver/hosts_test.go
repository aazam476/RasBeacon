package webserver

import (
	"net/http"
	"testing"

	"rasbeacon/internal/api"
)

// rbweb only answers under RasBeacon's own names, so a web page that points
// its own name at RasBeacon ("DNS rebinding") can't use the API.

func TestHosts_TheAPIAnswersOnlyUnderRasBeaconsNames(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	for _, host := range []string{"10.87.0.1", "10.87.0.1:80", "rasbeacon.lan", "RasBeacon.LAN", "localhost:8080", "127.0.0.1", "[::1]:8080"} {
		if rec := request(s, "GET", "http://"+host+"/api/v1/setup", "", nil, nil); rec.Code != http.StatusOK {
			t.Errorf("Host %s = %d, want 200", host, rec.Code)
		}
	}
	for _, host := range []string{"evil.example", "evil.example:80", "10.87.0.1.evil.example", "rasbeacon.lan.evil.example"} {
		for _, path := range []string{"/api/v1/setup", "/api/v1/session", "/"} {
			if rec := request(s, "GET", "http://"+host+path, "", nil, nil); rec.Code != http.StatusMisdirectedRequest {
				t.Errorf("Host %s%s = %d, want 421", host, path, rec.Code)
			}
		}
	}
	// Logging in under another name is refused before any password check.
	if rec := request(s, "POST", "http://evil.example/api/v1/session", `{"password":"guess"}`, map[string]string{"Content-Type": "application/json"}, nil); rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("login under another name = %d", rec.Code)
	}
	if len(s.limiter.failures) != 0 {
		t.Error("nothing should be counted")
	}
}

func TestHosts_TheOwnersNameForRasBeaconWorks(t *testing.T) {
	s, rbd := newTestServer(api.ModeNormal)
	rbd.hostname = "travel-pi.lan"
	if rec := request(s, "GET", "http://travel-pi.lan/api/v1/setup", "", nil, nil); rec.Code != http.StatusOK {
		t.Errorf("Host travel-pi.lan = %d", rec.Code)
	}
}

func TestHosts_PhonesChecksStillReachTheSetupPageAndNotice(t *testing.T) {
	for _, mode := range []struct {
		name   string
		setup  bool
		notice bool
	}{{"setup", true, false}, {"login-page notice", false, true}} {
		t.Run(mode.name, func(t *testing.T) {
			s, rbd := newTestServer(api.ModeNormal)
			if mode.setup {
				rbd.mode = api.ModeSetup
			}
			rbd.portalNotice = mode.notice
			rec := request(s, "GET", "http://captive.apple.com/hotspot-detect.html", "", nil, nil)
			if rec.Code != http.StatusFound {
				t.Errorf("a phone's check = %d, want the redirect", rec.Code)
			}
			// The API stays closed to other names even then.
			if rec := request(s, "GET", "http://captive.apple.com/api/v1/setup", "", nil, nil); rec.Code != http.StatusMisdirectedRequest {
				t.Errorf("the API under another name = %d, want 421", rec.Code)
			}
		})
	}
}
