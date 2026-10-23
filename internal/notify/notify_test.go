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

func TestDiscordGetsJSON(t *testing.T) {
	if !isDiscord("https://discord.com/api/webhooks/123/abc") || isDiscord("https://ntfy.sh/api/webhooks") {
		t.Error("only Discord's webhook addresses are Discord's")
	}
	body, _ := json.Marshal(map[string]string{"content": "x"})
	if !strings.Contains(string(body), `"content"`) {
		t.Fail()
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
