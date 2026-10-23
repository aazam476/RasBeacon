package agent

import (
	"strings"
	"testing"

	"rasbeacon/internal/api"
)

func TestWiFiQRText(t *testing.T) {
	for _, c := range []struct{ ssid, password, want string }{
		{"Ali's Wi-Fi", "kq7m-2xvd", "WIFI:T:WPA;S:Ali's Wi-Fi;P:kq7m-2xvd;;"},
		{`a;b,c:d"e\f`, `p;w`, `WIFI:T:WPA;S:a\;b\,c\:d\"e\\f;P:p\;w;;`},
		{"Open", "", "WIFI:T:nopass;S:Open;;"},
	} {
		if got := wifiQRText(c.ssid, c.password); got != c.want {
			t.Errorf("wifiQRText(%q, %q) = %q, want %q", c.ssid, c.password, got, c.want)
		}
	}
}

func TestWiFiQR_Hotspot(t *testing.T) {
	// Being logged in is enough (no admin password asked again).
	e := newClaimedEnv(t, false)
	code, err := e.agent.WiFiQR(api.WiFiQRRequest{Network: "hotspot"})
	if err != nil {
		t.Fatal(err)
	}
	if code.SSID == "" || !strings.HasPrefix(code.SVG, "<svg") || !strings.Contains(code.SVG, `fill="#000" d="M`) {
		t.Errorf("code = %+v", code)
	}
	if _, err := e.agent.WiFiQR(api.WiFiQRRequest{Network: "guest"}); err == nil {
		t.Error("the guest network is off")
	}
}
