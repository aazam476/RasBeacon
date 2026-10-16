// Package devsim makes rbd run on a normal computer without a Raspberry Pi,
// for developing and testing the web UI.
//
// Start rbd with `-dev /tmp/rasbeacon-dev` and it will:
//   - keep all its files under that directory instead of /,
//   - pretend there is a built-in radio, a USB Archer T3U and an Ethernet
//     port with no cable,
//   - pretend the built-in radio is connected to a café's Wi-Fi with
//     internet (IPv4 only), and that a scan finds a few networks,
//   - pretend a WireGuard tunnel, once set up, works,
//   - optionally run a simulated hotel login page (StartHotel) and use a
//     real headless Chromium as the login browser (see cmd/rbd's -dev-hotel
//     and -dev-browser options),
//   - run the speed test against a local stand-in for Cloudflare's service
//     (StartSpeedtestServer), at about 300 Mbps,
//   - log the commands it would run (ip, iw, systemctl, ...) instead of
//     running them, and answer with made-up output (two connected phones).
//
// Nothing on your computer's network is changed.
package devsim

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rasbeacon/internal/dhcpprobe"
	"rasbeacon/internal/paths"
	"rasbeacon/internal/portal"
	"rasbeacon/internal/speedtest"
)

// Setup prepares a fake device under root and returns its paths.
func Setup(root string) (paths.Paths, error) {
	p := paths.Paths{Root: root}

	// Fake /sys/class/net with a built-in radio (wlan0) and a USB radio (wlan1).
	radios := []struct{ iface, phy, device string }{
		{"wlan0", "phy0", "platform/mmc1/mmc1:0001:1"},
		{"wlan1", "phy1", "usb1/1-1/1-1:1.0"},
	}
	for _, r := range radios {
		ifaceDir := filepath.Join(p.SysClassNet(), r.iface)
		if err := writeFile(filepath.Join(ifaceDir, "phy80211", "name"), r.phy); err != nil {
			return p, err
		}
		device := filepath.Join(root, "sys", "devices", r.device)
		if err := os.MkdirAll(device, 0o755); err != nil {
			return p, err
		}
		link := filepath.Join(ifaceDir, "device")
		if _, err := os.Lstat(link); err != nil {
			if err := os.Symlink(device, link); err != nil {
				return p, err
			}
		}
	}
	usbDevice := filepath.Join(root, "sys", "devices", "usb1", "1-1")
	if err := writeFile(filepath.Join(usbDevice, "idVendor"), "2357"); err != nil {
		return p, err
	}
	if err := writeFile(filepath.Join(usbDevice, "idProduct"), "012d"); err != nil {
		return p, err
	}
	// The hotspot radio's MAC address (after rbd renames it to ap0).
	if err := writeFile(filepath.Join(p.SysClassNet(), "ap0", "address"), "50:3e:aa:12:34:56"); err != nil {
		return p, err
	}
	// Ethernet port, no cable plugged in.
	if err := writeFile(filepath.Join(p.SysClassNet(), "eth0", "carrier"), "0"); err != nil {
		return p, err
	}
	if err := writeFile(filepath.Join(p.SysClassNet(), "eth0", "carrier_changes"), "1"); err != nil {
		return p, err
	}
	if err := writeFile(p.DeviceTreeModel(), "Simulated Raspberry Pi (devsim)\x00"); err != nil {
		return p, err
	}
	if err := writeFile(p.ProcUptime(), "3725.00 1000.00"); err != nil {
		return p, err
	}
	if err := os.MkdirAll(p.BootConfigDir(), 0o755); err != nil {
		return p, err
	}
	// Hardware readings of a Raspberry Pi 4 for the System page (they don't
	// change, so the processor reads as idle).
	readings := map[string]string{
		p.ProcCPUInfo():    strings.Repeat("processor\t: 0\nCPU part\t: 0xd08\n\n", 4),
		p.ProcMeminfo():    "MemTotal: 3884332 kB\nMemAvailable: 3491116 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n",
		p.ProcStat():       "cpu  4705 356 584 3699 23 23 0 0 0 0\n",
		p.KernelRelease():  "6.12.34+rpt-rpi-v8\n",
		p.ThrottledFlags(): "0x0\n",
		filepath.Join(p.SysClassThermal(), "thermal_zone0", "temp"): "48750\n",
	}
	for path, content := range readings {
		if err := writeFile(path, content); err != nil {
			return p, err
		}
	}
	// Two phones with DHCP leases.
	leases := "1790800000 3a:1f:22:aa:bb:cc 10.87.0.23 Alis-iPhone *\n" +
		"1790800000 8e:00:11:22:33:44 10.87.0.24 Pixel-8 *\n"
	if err := writeFile(p.DnsmasqLeases(), leases); err != nil {
		return p, err
	}
	return p, nil
}

// Runner pretends to run commands: it logs them and returns canned output.
type Runner struct{}

// The simulated adapter can broadcast two networks (for the guest network).
const iwPhyInfo = "Wiphy phy1\n\tSupported interface modes:\n\t\t * managed\n\t\t * AP\n\t\t * monitor\n" +
	"\tvalid interface combinations:\n\t\t * #{ managed } <= 1, #{ AP } <= 2,\n\t\t   total <= 2, #channels <= 1\n"

const stationDump = `Station 3a:1f:22:aa:bb:cc (on ap0)
	signal:  	-48 [-50, -52] dBm
	connected time:	367 seconds
Station 8e:00:11:22:33:44 (on ap0)
	signal:  	-71 [-73, -74] dBm
	connected time:	42 seconds
`

// Made-up uplink: wlan0 is connected to "Corner Café" with 192.168.4.20.
const (
	defaultRoutes = `[{"dst":"default","gateway":"192.168.4.1","dev":"wlan0","protocol":"dhcp","metric":600,"flags":[]}]`
	addresses     = `[{"ifname":"wlan0","addr_info":[{"family":"inet","local":"192.168.4.20","prefixlen":24,"scope":"global"}]}]`
	wpaStatus     = "bssid=aa:bb:cc:dd:ee:ff\nid=0\nfreq=2437\nssid=Corner Caf\\xc3\\xa9\nkey_mgmt=WPA2-PSK\nwpa_state=COMPLETED\n"
	signalPoll    = "RSSI=-58\nLINKSPEED=72\n"
	scanResults   = "bssid / frequency / signal level / flags / ssid\n" +
		"aa:bb:cc:dd:ee:ff\t2437\t-58\t[WPA2-PSK-CCMP][ESS]\tCorner Caf\\xc3\\xa9\n" +
		"aa:bb:cc:dd:ee:01\t5180\t-63\t[ESS]\tHotel Guest\n" +
		"aa:bb:cc:dd:ee:02\t5500\t-71\t[WPA2-EAP-CCMP][ESS]\teduroam\n" +
		"aa:bb:cc:dd:ee:03\t2462\t-80\t[RSN-SAE-CCMP][ESS]\tNeighbour 5G\n"
)

// DHCPProbe pretends no DHCP server answers on the Ethernet port, so the
// simulated client port connects. Nothing is sent on a real network.
func DHCPProbe(iface string, timeout time.Duration) (dhcpprobe.Result, error) {
	slog.Info("devsim: would check for a DHCP server", "interface", iface)
	return dhcpprobe.Result{}, nil
}

// PortalProbe stands in for the captive portal check. The simulated
// network has no login page, unless the simulated hotel (StartHotel) was
// told to require one.
func PortalProbe(ctx context.Context) portal.Result {
	hotel.mu.Lock()
	defer hotel.mu.Unlock()
	if hotel.loginRequired {
		return portal.Result{State: portal.Portal, LoginURL: "http://" + hotel.address + "/"}
	}
	return portal.Result{State: portal.Open}
}

// hotel is the simulated hotel network's login page (see StartHotel).
var hotel struct {
	mu            sync.Mutex
	address       string
	loginRequired bool
}

// StartHotel runs a simulated hotel login page on address (for example
// 127.0.0.1:8099), for trying the login browser without a real hotel:
//
//	GET /require-login   from now on, the network "needs a login"
//	GET /                the login page (a room number and a button)
//	GET /login?room=...  logs in: the network is open again
func StartHotel(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	hotel.mu.Lock()
	hotel.address = listener.Addr().String()
	hotel.mu.Unlock()

	setRequired := func(required bool) {
		hotel.mu.Lock()
		hotel.loginRequired = required
		hotel.mu.Unlock()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /require-login", func(w http.ResponseWriter, r *http.Request) {
		setRequired(true)
		fmt.Fprintln(w, "the simulated hotel network now needs a login")
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, hotelLoginPage)
	})
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		setRequired(false)
		fmt.Fprintf(w, hotelWelcomePage, html.EscapeString(r.URL.Query().Get("room")))
	})
	slog.Info("devsim: simulated hotel login page", "address", hotel.address)
	go http.Serve(listener, mux)
	return nil
}

const hotelLoginPage = `<!doctype html>
<meta name="viewport" content="width=device-width">
<title>Grand Hotel Wi-Fi</title>
<style>
body{font-family:sans-serif;margin:24px}
h1{margin:0;font-size:28px}
#room{position:absolute;left:24px;top:120px;width:300px;height:40px;font-size:20px}
button{position:absolute;left:24px;top:180px;font-size:20px}
</style>
<h1>Grand Hotel Wi-Fi</h1>
<form action="/login">
<label>Room number <input name="room" id="room" autocomplete="off"></label>
<button>Connect</button>
</form>`

const hotelWelcomePage = `<!doctype html>
<meta name="viewport" content="width=device-width">
<title>Connected</title>
<h1>You are online</h1><p>Enjoy your stay, room %s.</p>`

// StartSpeedtestServer runs a stand-in for Cloudflare's speed test service
// on a free local port and returns its address. The speed test (with the
// -dev-browser) measures it instead of the internet.
func StartSpeedtestServer() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	standIn := &speedtest.StandIn{RateMbps: 300}
	go http.Serve(listener, standIn.Handler())
	address := "http://" + listener.Addr().String()
	slog.Info("devsim: simulated speed test service", "address", address)
	return address, nil
}

// Run logs the command and returns made-up output where it matters.
func (Runner) Run(name string, args ...string) (string, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	slog.Info("devsim: would run", "command", line)
	switch {
	case strings.HasPrefix(line, "iw phy") && strings.HasSuffix(line, "info"):
		return iwPhyInfo, nil
	case strings.HasPrefix(line, "iw dev ap0 station dump"):
		return stationDump, nil
	case strings.HasPrefix(line, "systemctl is-active"):
		return "active\n", nil
	case line == "ip -json route show default":
		return defaultRoutes, nil
	case line == "ip -json -6 route show default":
		return "[]", nil
	case line == "ip -json address show":
		return addresses, nil
	case strings.HasPrefix(line, "wpa_cli") && strings.HasSuffix(line, " status"):
		return wpaStatus, nil
	case strings.HasPrefix(line, "wpa_cli") && strings.HasSuffix(line, " signal_poll"):
		return signalPoll, nil
	case strings.HasPrefix(line, "wpa_cli") && strings.HasSuffix(line, " scan_results"):
		return scanResults, nil
	case line == "wg show wg0 dump":
		// A tunnel that just completed a handshake.
		return fmt.Sprintf("priv\tpub\t0\toff\nxTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\t(none)\t203.0.113.5:51820\t0.0.0.0/0,::/0\t%d\t48213\t20981\t25\n",
			time.Now().Unix()-20), nil
	case strings.HasPrefix(line, "ip link show"):
		return "", fmt.Errorf("devsim: link does not exist")
	}
	return "", nil
}

// writeFile writes a file, making its folders first.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
