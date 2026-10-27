package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"rasbeacon/internal/api"
)

// fakeWelcomeRBD answers the welcome page's two questions like rbd: what to
// show the device, and whether its "I agree" (with code) is accepted.
type fakeWelcomeRBD struct {
	page     api.WelcomePage
	code     string
	accepted []api.WelcomeAcceptRequest
	down     bool
}

func (f *fakeWelcomeRBD) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /welcome/page", func(w http.ResponseWriter, r *http.Request) {
		if f.down {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		page := f.page
		if r.URL.Query().Get("ip") != "10.87.1.23" {
			page = api.WelcomePage{}
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("POST /welcome/accept", func(w http.ResponseWriter, r *http.Request) {
		var req api.WelcomeAcceptRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if f.code != "" && req.Code != f.code {
			writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "That code isn't right.", Field: "code"})
			return
		}
		f.accepted = append(f.accepted, req)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// welcomeRequest sends a request to the welcome server from a guest's
// phone at 10.87.1.23.
func welcomeRequest(f *fakeWelcomeRBD, method, target string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	s := New(NewRBDClientForHandler(f.handler()), fstest.MapFS{})
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = "10.87.1.23:50000"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.WelcomeHandler().ServeHTTP(rec, req)
	return rec
}

func TestWelcome_ShowsThePageForAnyAddress(t *testing.T) {
	f := &fakeWelcomeRBD{page: api.WelcomePage{
		Active: true, Title: "Hôtel <Lumière>", Message: "Rules\nbelow", Terms: "Be kind.", NeedCode: true,
		Color: "#ff3b30", Logo: "data:image/png;base64,iVBORw0KGgo=",
	}}
	// A phone checking for internet (captive.apple.com) lands here too.
	rec := welcomeRequest(f, "GET", "http://captive.apple.com/hotspot-detect.html", nil, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{
		"<h1>Hôtel &lt;Lumière&gt;</h1>", "Be kind.", `name="agree"`, `name="code"`, "--accent: #ff3b30",
		`src="data:image/png;base64,iVBORw0KGgo="`, "Accept and continue",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in the page", want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Errorf("policy = %q", csp)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the page must not be cached")
	}
}

func TestWelcome_AcceptsWithTheTermsAndCode(t *testing.T) {
	f := &fakeWelcomeRBD{page: api.WelcomePage{Active: true, Title: "Welcome", Terms: "Be kind.", NeedCode: true}, code: "4711"}

	// Without the box ticked: asked again, nothing sent to rbd.
	rec := welcomeRequest(f, "POST", "http://10.87.1.1:2050/accept", url.Values{"code": {"4711"}}, nil)
	if !strings.Contains(rec.Body.String(), "Tick the box") || len(f.accepted) != 0 {
		t.Fatalf("unticked box: %s", rec.Body.String())
	}
	// A wrong code: rbd's message on the page.
	rec = welcomeRequest(f, "POST", "http://10.87.1.1:2050/accept", url.Values{"agree": {"on"}, "code": {"0000"}}, nil)
	if !strings.Contains(rec.Body.String(), "That code isn") {
		t.Fatalf("wrong code: %s", rec.Body.String())
	}
	// Right: rbd is told the device's address and code; "You're connected".
	rec = welcomeRequest(f, "POST", "http://10.87.1.1:2050/accept", url.Values{"agree": {"on"}, "code": {" 4711 "}},
		map[string]string{"Origin": "http://10.87.1.1:2050"})
	if !strings.Contains(rec.Body.String(), "You’re connected") ||
		len(f.accepted) != 1 || f.accepted[0] != (api.WelcomeAcceptRequest{IP: "10.87.1.23", Code: "4711"}) {
		t.Fatalf("accepted = %+v, page: %s", f.accepted, rec.Body.String())
	}
}

func TestWelcome_RefusesOtherWebsitesForms(t *testing.T) {
	f := &fakeWelcomeRBD{page: api.WelcomePage{Active: true, Title: "Welcome"}}
	rec := welcomeRequest(f, "POST", "http://10.87.1.1:2050/accept", url.Values{}, map[string]string{"Origin": "http://evil.example"})
	if rec.Code != http.StatusForbidden || len(f.accepted) != 0 {
		t.Errorf("status %d, accepted %v", rec.Code, f.accepted)
	}
}

func TestWelcome_TheOwnersOwnPageIsSandboxed(t *testing.T) {
	custom := `<form method="post" action="/accept"><button>OK</button></form><script>alert(1)</script>`
	f := &fakeWelcomeRBD{page: api.WelcomePage{Active: true, Title: "Welcome", CustomPage: custom}}
	rec := welcomeRequest(f, "GET", "http://example.com/", nil, nil)
	if rec.Body.String() != custom {
		t.Errorf("the page should be served as it is: %q", rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox allow-forms;") || strings.Contains(csp, "allow-scripts") {
		t.Errorf("policy = %q", csp)
	}
	// Its form accepts without a box of ours.
	welcomeRequest(f, "POST", "http://10.87.1.1:2050/accept", url.Values{}, nil)
	if len(f.accepted) != 1 {
		t.Error("the owner's page decides how it asks")
	}
}

func TestWelcome_DoneOrNotNeededOrDown(t *testing.T) {
	// Already accepted, or a device without the page: "You're connected".
	f := &fakeWelcomeRBD{page: api.WelcomePage{Active: true, Accepted: true, Title: "Welcome"}}
	if rec := welcomeRequest(f, "GET", "/", nil, nil); !strings.Contains(rec.Body.String(), "You’re connected") {
		t.Error("an accepted device should see that it is connected")
	}
	if rec := welcomeRequest(f, "POST", "/accept", url.Values{}, nil); !strings.Contains(rec.Body.String(), "You’re connected") || len(f.accepted) != 0 {
		t.Error("accepting again does nothing")
	}
	f.down = true
	if rec := welcomeRequest(f, "GET", "/", nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("rbd down: status %d", rec.Code)
	}
	if rec := welcomeRequest(f, "POST", "/accept", url.Values{}, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("rbd down: status %d", rec.Code)
	}
}
