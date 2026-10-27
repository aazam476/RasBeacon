package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSend_PlainTextWithNtfyHeaders(t *testing.T) {
	var body, title, click string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body, title, click = string(data), r.Header.Get("Title"), r.Header.Get("Click")
	}))
	defer server.Close()
	err := Send(context.Background(), server.Client(), server.URL+"/rasbeacon-updates", "RasBeacon v0.3.0", "A new version is out.", "https://example.com/v0.3.0")
	if err != nil || body != "A new version is out." || title != "RasBeacon v0.3.0" || click != "https://example.com/v0.3.0" {
		t.Errorf("err %v, body %q, title %q, click %q", err, body, title, click)
	}
}

func TestSend_ReportsRefusals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer server.Close()
	if err := Send(context.Background(), server.Client(), server.URL, "t", "m", ""); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("got %v", err)
	}
}

// recorder is an HTTP client's transport that answers 204 to every
// request and keeps the last one, so a test can send to any address
// (discord.com, say) without the network.
type recorder struct {
	req  *http.Request
	body string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	data, _ := io.ReadAll(req.Body)
	r.req, r.body = req, string(data)
	return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func TestDiscordGetsJSON(t *testing.T) {
	if !isDiscord("https://discord.com/api/webhooks/123/abc") || isDiscord("https://ntfy.sh/api/webhooks") {
		t.Error("only Discord's webhook addresses are Discord's")
	}
	rec := &recorder{}
	err := Send(context.Background(), &http.Client{Transport: rec}, "https://discord.com/api/webhooks/123/abc",
		"RasBeacon v0.3.0", "A new version is out.", "https://example.com/v0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if rec.req.Header.Get("Content-Type") != "application/json" || rec.req.Header.Get("Title") != "" {
		t.Errorf("headers %v, want JSON without ntfy's headers", rec.req.Header)
	}
	var message map[string]string
	if err := json.Unmarshal([]byte(rec.body), &message); err != nil {
		t.Fatalf("Discord needs JSON: %q (%v)", rec.body, err)
	}
	if want := "**RasBeacon v0.3.0**\nA new version is out.\nhttps://example.com/v0.3.0"; message["content"] != want || len(message) != 1 {
		t.Errorf("message = %q, want {content: %q}", message, want)
	}
}

func TestCheckURL(t *testing.T) {
	for address, ok := range map[string]bool{
		"https://ntfy.sh/topic": true, "http://192.168.1.5/topic": true,
		"ftp://example.com": false, "ntfy.sh/topic": false, "": false,
	} {
		if (CheckURL(address) == nil) != ok {
			t.Errorf("CheckURL(%q)", address)
		}
	}
}
