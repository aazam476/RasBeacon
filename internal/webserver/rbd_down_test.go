package webserver

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"rasbeacon/internal/api"
)

// When rbd can't check the password (it answers with an error), rbweb says
// so with 502 Bad Gateway: it neither logs the browser in nor calls the
// password wrong.
func TestLogin_WhenRBDCantCheckThePassword(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, api.SetupState{Mode: api.ModeNormal, LANAddress: "10.87.0.1"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "restarting", http.StatusServiceUnavailable)
	})
	s := New(NewRBDClientForHandler(mux), fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})

	rec := request(s, "POST", "http://10.87.0.1/api/v1/session", `{"password":"tangerine-orbit-42"}`, nil, nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "could not check the password") {
		t.Errorf("login = %d %s", rec.Code, rec.Body)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("no session may be given when the password wasn't checked")
	}
}

func TestLogin_RefusesABodyThatIsntJSON(t *testing.T) {
	s, _ := newTestServer(api.ModeNormal)
	rec := request(s, "POST", "http://10.87.0.1/api/v1/session", `{"password":`, nil, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid request") {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}
