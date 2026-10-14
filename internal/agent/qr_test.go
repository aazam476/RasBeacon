package agent

import (
	"errors"
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

func TestWiFiQR_HotspotNeedsTheAdminPassword(t *testing.T) {
	e := newClaimedEnv(t, false)
	if _, err := e.agent.WiFiQR(api.WiFiQRRequest{Network: "hotspot", AdminPassword: "wrong"}); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("got %v", err)
	}
	code, err := e.agent.WiFiQR(api.WiFiQRRequest{Network: "hotspot", AdminPassword: validClaim.AdminPassword})
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
