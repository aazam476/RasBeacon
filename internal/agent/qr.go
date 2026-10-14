package agent

// This file makes the QR codes phones scan to join the hotspot or the guest
// network without typing (the "WIFI:" format phones' cameras understand).

import (
	"fmt"
	"strings"

	"rsc.io/qr"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// wifiQRText is the text in a Wi-Fi QR code: WIFI:T:WPA;S:<name>;P:<password>;;
// The characters \ ; , : and " in the name and password are escaped with a
// backslash.
func wifiQRText(ssid, password string) string {
	escape := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`)
	if password == "" {
		return "WIFI:T:nopass;S:" + escape.Replace(ssid) + ";;"
	}
	// "WPA" covers WPA2 and WPA3 (phones pick the best both support).
	return "WIFI:T:WPA;S:" + escape.Replace(ssid) + ";P:" + escape.Replace(password) + ";;"
}

// qrSVG draws text as a QR code: black squares on white with the standard
// quiet zone around them, as an SVG that scales to any size.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := code.Size + 2*quiet
	var path strings.Builder
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		size, size, size, size, path.String()), nil
}

// WiFiQR returns the QR code for joining the hotspot ("hotspot", which
// needs the admin password, like showing its password) or the guest network
// ("guest", whose password is meant to be handed out).
func (a *Agent) WiFiQR(req api.WiFiQRRequest) (api.WiFiQR, error) {
	if req.Network == "hotspot" && !a.VerifyAdminPassword(req.AdminPassword) {
		return api.WiFiQR{}, ErrWrongPassword
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var ssid, password string
	switch req.Network {
	case "hotspot":
		ssid, password = a.cfg.Hotspot.SSID, a.secrets.HotspotPassword
	case "guest":
		if !a.cfg.Guest.Enabled {
			return api.WiFiQR{}, &config.ValidationError{Field: "network", Message: "The guest network is off."}
		}
		ssid, password = a.cfg.Guest.SSID, a.secrets.GuestPassword
	default:
		return api.WiFiQR{}, &config.ValidationError{Field: "network", Message: "Choose the hotspot or the guest network."}
	}
	svg, err := qrSVG(wifiQRText(ssid, password))
	if err != nil {
		return api.WiFiQR{}, err
	}
	return api.WiFiQR{SSID: ssid, SVG: svg}, nil
}
