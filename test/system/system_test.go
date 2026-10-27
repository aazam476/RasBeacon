// Package system is RasBeacon's full-system test: the real rbd and rbweb,
// started by systemd with hostapd, wpa_supplicant, dnsmasq, nftables and
// WireGuard, in a virtual machine with simulated Wi-Fi radios
// (mac80211_hwsim). See README.md next to this file for how it runs.
//
// The test program has four entry points, each a test function:
//
//   - TestWorld runs at boot, before rbd: it builds the simulated world
//     (a hotel with a login page and a WireGuard server, and three phones).
//   - TestScenarios runs once RasBeacon is up: the scenarios themselves.
//   - TestHotel and TestFetch are helpers the other two start inside a
//     network namespace: the hotel's web servers, and one web request made
//     from a phone.
//
// Outside the virtual machine every one of them is skipped, so `go test
// ./...` passes over this package.
package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The simulated world. RasBeacon keeps the first two radios (wlan0 runs
// the hotspot, wlan1 joins the hotel); the others move into the hotel's
// and the phones' network namespaces.
const (
	stateDir  = "/run/rasbeacon-test" // keys and files TestWorld leaves for TestScenarios
	hotelNS   = "hotel"
	hotelIF   = "wlan2"
	hotelSSID = "Grand Hotel"
	// hotelLAN is the hotel's own address on its Wi-Fi, where its login
	// page is; internetIP stands for the whole internet (every name the
	// hotel's DNS is asked for points there).
	hotelLAN   = "192.168.50.1"
	internetIP = "203.0.113.1"
	// The WireGuard server, in the hotel: its address inside the tunnel,
	// and RasBeacon's.
	vpnServerIP = "10.66.0.1"
	vpnClientIP = "10.66.0.2"

	adminPassword   = "full-system-admin"
	hotspotSSID     = "Test Travel Wi-Fi"
	hotspotPassword = "full-system-wifi"
	guestSSID       = "Test Travel Guests"
	guestPassword   = "full-system-guest"
)

// phone is a simulated phone: a radio in its own network namespace.
type phone struct {
	ns, iface string
}

var phones = []phone{{"phone1", "wlan3"}, {"phone2", "wlan4"}, {"phone3", "wlan5"}}

// --- Running commands ---

// run runs a command (inside network namespace ns, unless ns is "") and
// returns its output, failing the test if it fails.
func run(t *testing.T, ns string, args ...string) string {
	t.Helper()
	out, err := tryRun(ns, args...)
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// tryRun is run without failing the test.
func tryRun(ns string, args ...string) (string, error) {
	if ns != "" {
		args = append([]string{"ip", "netns", "exec", ns}, args...)
	}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	return string(out), err
}

// startBackground starts a long-running program (a server) inside ns and
// leaves it running after this test program exits.
func startBackground(t *testing.T, ns string, env []string, args ...string) {
	t.Helper()
	if ns != "" {
		args = append([]string{"ip", "netns", "exec", ns}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), env...)
	log, err := os.Create(filepath.Join(stateDir, filepath.Base(args[len(args)-1])+".log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %v: %v", args, err)
	}
}

// waitFor checks every second until check succeeds or timeout passes. Each
// failed check says why, and the last reason goes into the failure. No
// step of the test waits a fixed time.
func waitFor(t *testing.T, what string, timeout time.Duration, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		ok, why := check()
		if ok {
			return
		}
		last = why
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s (last: %s)", timeout, what, last)
		}
		time.Sleep(time.Second)
	}
}

// --- The world (runs at boot, before rbd) ---

// TestWorld builds the simulated world. It runs as a systemd service
// before rbd starts (see units/rasbeacon-test-world.service), so RasBeacon
// only ever sees its own two radios.
func TestWorld(t *testing.T) {
	if os.Getenv("INVOCATION_ID") == "" || os.Getuid() != 0 {
		t.Skip("runs only inside the full-system test's virtual machine (see README.md)")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Six simulated radios: wlan0 to wlan5.
	run(t, "", "modprobe", "mac80211_hwsim", "radios=6")
	waitFor(t, "the simulated radios", 30*time.Second, func() (bool, string) {
		_, err := os.Stat("/sys/class/net/wlan5")
		return err == nil, "no wlan5 yet"
	})

	// The hotel and the phones each get a namespace and one radio. A radio
	// moves by its "phy" name.
	for _, p := range append([]phone{{hotelNS, hotelIF}}, phones...) {
		run(t, "", "ip", "netns", "add", p.ns)
		run(t, "", "ip", "netns", "exec", p.ns, "ip", "link", "set", "lo", "up")
		phy, err := os.ReadFile("/sys/class/net/" + p.iface + "/phy80211/name")
		if err != nil {
			t.Fatal(err)
		}
		run(t, "", "iw", "phy", strings.TrimSpace(string(phy)), "set", "netns", "name", p.ns)
	}

	buildHotel(t)
	t.Log("the simulated world is ready")
}

// buildHotel sets up the hotel: an open Wi-Fi network with DHCP and DNS,
// a login page that every web request is sent to until the visitor
// accepts the terms, "the internet" behind it, and a WireGuard server.
func buildHotel(t *testing.T) {
	ns := hotelNS
	run(t, ns, "ip", "address", "add", hotelLAN+"/24", "dev", hotelIF)
	run(t, ns, "ip", "link", "set", hotelIF, "up")
	// The internet: one address on a dummy interface.
	run(t, ns, "ip", "link", "add", "internet", "type", "dummy")
	run(t, ns, "ip", "address", "add", internetIP+"/32", "dev", "internet")
	run(t, ns, "ip", "link", "set", "internet", "up")

	// The WireGuard server, with keys made now. RasBeacon's private key is
	// left for TestScenarios, which imports it as a WireGuard file.
	serverKey := strings.TrimSpace(run(t, "", "wg", "genkey"))
	clientKey := strings.TrimSpace(run(t, "", "wg", "genkey"))
	serverPub := pubKey(t, serverKey)
	clientPub := pubKey(t, clientKey)
	keyFile := filepath.Join(stateDir, "server.key")
	if err := os.WriteFile(keyFile, []byte(serverKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, ns, "ip", "link", "add", "wgs", "type", "wireguard")
	run(t, ns, "wg", "set", "wgs", "listen-port", "51820", "private-key", keyFile,
		"peer", clientPub, "allowed-ips", vpnClientIP+"/32")
	run(t, ns, "ip", "address", "add", vpnServerIP+"/24", "dev", "wgs")
	run(t, ns, "ip", "link", "set", "wgs", "up")
	conf := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\nDNS = %s\n\n[Peer]\nPublicKey = %s\nEndpoint = %s:51820\nAllowedIPs = 0.0.0.0/0\n",
		clientKey, vpnClientIP, vpnServerIP, serverPub, internetIP)
	if err := os.WriteFile(filepath.Join(stateDir, "vpn.conf"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	// Until a visitor accepts the terms, the internet is out of reach:
	// web requests to it go to the login page instead, and everything else
	// (the WireGuard server too) is dropped. Accepting adds the visitor's
	// address to the "authorized" set.
	rules := fmt.Sprintf(`table inet hotel {
	set authorized { type ipv4_addr; }
	chain prerouting {
		type nat hook prerouting priority dstnat;
		iifname "%[1]s" ip daddr %[2]s tcp dport 80 ip saddr != @authorized dnat ip to %[3]s:8080
	}
	chain input {
		type filter hook input priority filter; policy accept;
		iifname "%[1]s" ip daddr %[2]s ip saddr != @authorized drop
	}
}
`, hotelIF, internetIP, hotelLAN)
	rulesFile := filepath.Join(stateDir, "hotel.nft")
	if err := os.WriteFile(rulesFile, []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, ns, "nft", "-f", rulesFile)

	// The hotel's Wi-Fi (open), its DHCP and DNS (every name is "the
	// internet"; the tunnel's DNS server is the same program), and its web
	// servers.
	hostapdConf := filepath.Join(stateDir, "hotel-hostapd.conf")
	os.WriteFile(hostapdConf, []byte(fmt.Sprintf("interface=%s\ndriver=nl80211\nssid=%s\nhw_mode=g\nchannel=11\n", hotelIF, hotelSSID)), 0o600)
	startBackground(t, ns, nil, "hostapd", hostapdConf)
	startBackground(t, ns, nil, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--interface="+hotelIF, "--interface=wgs", "--bind-dynamic",
		"--dhcp-range=192.168.50.10,192.168.50.100,12h", "--dhcp-leasefile="+filepath.Join(stateDir, "hotel.leases"),
		"--address=/#/"+internetIP, "--pid-file=", "--log-facility=-")
	self, _ := os.Executable()
	startBackground(t, ns, []string{"RASBEACON_HOTEL=1"}, self, "-test.run=^TestHotel$", "hotel")
}

// pubKey is a WireGuard private key's public key.
func pubKey(t *testing.T, private string) string {
	t.Helper()
	cmd := exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(private + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// --- Helpers that run inside a namespace ---

// TestHotel runs the hotel's two web servers until the machine stops: the
// login page (on port 8080, where every web request from a visitor who
// hasn't accepted the terms ends up), and the internet (port 80), which
// answers the phones' and RasBeacon's connectivity checks and tells each
// visitor the address it came from.
func TestHotel(t *testing.T) {
	if os.Getenv("RASBEACON_HOTEL") != "1" {
		t.Skip("started by TestWorld inside the hotel's namespace")
	}
	login := http.NewServeMux()
	login.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		// One big button, so a click anywhere in the login browser's
		// picture accepts.
		fmt.Fprint(w, `<!doctype html><title>Grand Hotel</title>
<form method="post" action="/accept"><button style="position:fixed;inset:0;width:100%;height:100%;font-size:40px">Accept and connect</button></form>`)
	})
	login.HandleFunc("POST /accept", func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if out, err := exec.Command("nft", "add", "element", "inet", "hotel", "authorized", "{", ip, "}").CombinedOutput(); err != nil {
			http.Error(w, string(out), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "<!doctype html><title>Connected</title>You are online.")
	})
	login.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+hotelLAN+":8080/login", http.StatusFound)
	})

	internet := http.NewServeMux()
	internet.HandleFunc("/generate_204", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	internet.HandleFunc("/hotspot-detect.html", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>")
	})
	internet.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		fmt.Fprint(w, ip)
	})

	// Each request is logged (the file is shown when a scenario fails).
	logged := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Println(r.RemoteAddr, r.Method, r.Host, r.URL.Path)
			h.ServeHTTP(w, r)
		})
	}
	errs := make(chan error, 2)
	go func() { errs <- http.ListenAndServe(hotelLAN+":8080", logged(login)) }()
	go func() { errs <- http.ListenAndServe(internetIP+":80", logged(internet)) }()
	t.Fatal(<-errs)
}

// TestFetch makes one web request, from inside a phone's namespace:
// RASBEACON_FETCH holds the method, address and body as JSON. It prints
// the answer's status and body on the last two lines.
func TestFetch(t *testing.T) {
	text := os.Getenv("RASBEACON_FETCH")
	if text == "" {
		t.Skip("started by TestScenarios inside a phone's namespace")
	}
	var req struct{ Method, URL, Body string }
	if err := json.Unmarshal([]byte(text), &req); err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(req.Method, req.URL, strings.NewReader(req.Body))
	if req.Body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // report a redirect as it is
	}}
	resp, err := client.Do(r)
	if err != nil {
		fmt.Printf("\nSTATUS 0\nERROR %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("\nSTATUS %d\nBODY %s\n", resp.StatusCode, strings.ReplaceAll(string(body), "\n", " "))
}

// --- Phones ---

// fetch makes a web request from phone p: the answer's status (0 when no
// answer came) and body.
func (p phone) fetch(method, url, body string) (int, string) {
	self, _ := os.Executable()
	req, _ := json.Marshal(map[string]string{"Method": method, "URL": url, "Body": body})
	cmd := exec.Command("ip", "netns", "exec", p.ns, self, "-test.run=^TestFetch$")
	cmd.Env = append(os.Environ(), "RASBEACON_FETCH="+string(req))
	out, _ := cmd.CombinedOutput()
	status, answer := 0, ""
	for _, line := range strings.Split(string(out), "\n") {
		if s, ok := strings.CutPrefix(line, "STATUS "); ok {
			fmt.Sscan(s, &status)
		}
		if b, ok := strings.CutPrefix(line, "BODY "); ok {
			answer = b
		}
		if b, ok := strings.CutPrefix(line, "ERROR "); ok {
			answer = b
		}
	}
	return status, answer
}

// join connects phone p to a Wi-Fi network: WPA2 with a password, WPA3
// (sae true) or open (no password). It doesn't wait; see connected.
func (p phone) join(t *testing.T, ssid, password string, sae bool) {
	t.Helper()
	network := fmt.Sprintf("network={\n\tssid=%q\n\tkey_mgmt=NONE\n}\n", ssid)
	switch {
	case sae:
		network = fmt.Sprintf("network={\n\tssid=%q\n\tkey_mgmt=SAE\n\tsae_password=%q\n\tieee80211w=2\n}\n", ssid, password)
	case password != "":
		network = fmt.Sprintf("network={\n\tssid=%q\n\tkey_mgmt=WPA-PSK\n\tpsk=%q\n}\n", ssid, password)
	}
	conf := filepath.Join(stateDir, p.ns+"-wpa.conf")
	if err := os.WriteFile(conf, []byte("ctrl_interface=/run/wpa_supplicant-"+p.ns+"\n"+network), 0o600); err != nil {
		t.Fatal(err)
	}
	p.leave()
	run(t, p.ns, "wpa_supplicant", "-B", "-i", p.iface, "-c", conf, "-P", filepath.Join(stateDir, p.ns+"-wpa.pid"))
}

// leave disconnects phone p from Wi-Fi: it asks its wpa_supplicant to
// stop and waits until it has (up to 10 seconds), so the next one can
// take over the radio.
func (p phone) leave() {
	pidFile := filepath.Join(stateDir, p.ns+"-wpa.pid")
	if data, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(data))
		tryRun(p.ns, "wpa_cli", "-p", "/run/wpa_supplicant-"+p.ns, "-i", p.iface, "terminate")
		for range 100 {
			if _, err := os.Stat("/proc/" + pid); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		exec.Command("kill", "-9", pid).Run() // if it didn't stop by itself
		os.Remove(pidFile)
	}
	os.RemoveAll("/run/wpa_supplicant-" + p.ns)
	tryRun(p.ns, "ip", "address", "flush", "dev", p.iface)
}

// associated reports whether phone p has joined its network.
func (p phone) associated() (bool, string) {
	out, _ := tryRun(p.ns, "wpa_cli", "-p", "/run/wpa_supplicant-"+p.ns, "-i", p.iface, "status")
	return strings.Contains(out, "wpa_state=COMPLETED"), strings.TrimSpace(out)
}

// connected waits until phone p has joined and got an address by DHCP, and
// returns the address.
func (p phone) connected(t *testing.T) string {
	t.Helper()
	waitFor(t, p.ns+" to join", 60*time.Second, p.associated)
	// busybox's DHCP client gets a lease; the test sets it itself (with
	// /bin/true as the client's script, so the client changes nothing).
	dhcp := run(t, p.ns, "busybox", "udhcpc", "-i", p.iface, "-n", "-q", "-t", "10", "-T", "2", "-s", "/bin/true")
	match := regexp.MustCompile(`lease of (\S+) obtained from (\S+),`).FindStringSubmatch(dhcp)
	if match == nil {
		t.Fatalf("%s got no address by DHCP:\n%s", p.ns, dhcp)
	}
	address, router := match[1], match[2]
	run(t, p.ns, "ip", "address", "flush", "dev", p.iface)
	run(t, p.ns, "ip", "address", "add", address+"/24", "dev", p.iface)
	run(t, p.ns, "ip", "route", "replace", "default", "via", router, "dev", p.iface)
	return address
}

// mac is phone p's Wi-Fi MAC address.
func (p phone) mac(t *testing.T) string {
	return strings.TrimSpace(run(t, p.ns, "cat", "/sys/class/net/"+p.iface+"/address"))
}

// --- rbd's API (on its socket, as rbweb uses it) ---

var rbdClient = &http.Client{
	Timeout: 2 * time.Minute,
	Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/run/rbd/api.sock")
	}},
}

// rbd calls one of rbd's routes with a JSON body (or none) and decodes the
// answer into out (unless nil). It fails the test unless rbd answers 2xx.
func rbd(t *testing.T, method, path string, body, out any) {
	t.Helper()
	if err := tryRBD(method, path, body, out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
}

func tryRBD(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, "http://rbd"+path, reader)
	resp, err := rbdClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, data)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// status is the part of rbd's status the scenarios look at.
type status struct {
	Mode    string `json:"mode"`
	Hotspot struct {
		Standard string `json:"standard"`
	} `json:"hotspot"`
	Uplink struct {
		Connected bool   `json:"connected"`
		Interface string `json:"interface"`
		// Address is RasBeacon's address on the uplink, maybe with its
		// network size ("192.168.50.23/24").
		Address string `json:"address"`
	} `json:"uplink"`
	VPN struct {
		State string `json:"state"`
	} `json:"vpn"`
	Portal struct {
		Detected bool `json:"detected"`
	} `json:"portal"`
}

func rbdStatus(t *testing.T) status {
	t.Helper()
	var s status
	rbd(t, "GET", "/status", nil, &s)
	return s
}

// --- The scenarios ---

// TestScenarios runs the scenarios in order; each builds on the ones
// before, so the first failure stops the run.
func TestScenarios(t *testing.T) {
	if os.Getenv("RASBEACON_SYSTEM_TEST") != "1" {
		t.Skip("runs only inside the full-system test's virtual machine (see README.md)")
	}
	waitFor(t, "rbd's API", 2*time.Minute, func() (bool, string) {
		err := tryRBD("GET", "/state", nil, nil)
		return err == nil, fmt.Sprint(err)
	})
	steps := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"1 set RasBeacon up from a phone", setUp},
		{"2 log into the hotel through the login browser", hotelLogin},
		{"3 the VPN carries the traffic and the kill switch holds", vpnAndKillSwitch},
		{"4 guests reach the internet and nothing else", guests},
		{"5 the allow list keeps out an unlisted phone", allowList},
		{"6 a phone joins with WPA3 only", wpa3Only},
		{"7 rbd restarts with the guest network on", restartWithGuests},
		{"8 the self-check repairs the hotspot", selfCheck},
		{"9 the Wi-Fi uplink joins with a cloned MAC address", clonedMAC},
	}
	for _, step := range steps {
		if !t.Run(step.name, step.run) {
			t.Fatalf("stopping: %q failed", step.name)
		}
	}
}

// 1. A phone joins the open setup network and claims RasBeacon through
// the web page's API (rbweb), then joins the new hotspot.
func setUp(t *testing.T) {
	p := phones[0]
	run(t, p.ns, "ip", "link", "set", p.iface, "up")
	setupSSID := ""
	waitFor(t, "the setup network", 90*time.Second, func() (bool, string) {
		out, _ := tryRun(p.ns, "iw", "dev", p.iface, "scan")
		match := regexp.MustCompile(`SSID: (RasBeacon-Setup-\S+)`).FindStringSubmatch(out)
		if match == nil {
			return false, "not seen in a scan"
		}
		setupSSID = match[1]
		return true, ""
	})
	p.join(t, setupSSID, "", false)
	address := p.connected(t)
	t.Logf("phone1 joined %s and got %s", setupSSID, address)

	claim := fmt.Sprintf(`{"adminPassword":%q,"hotspotSsid":%q,"hotspotPassword":%q,"hotspotBand":"2.4GHz"}`,
		adminPassword, hotspotSSID, hotspotPassword)
	code, body := p.fetch("POST", "http://10.87.0.1/api/v1/setup/claim", claim)
	if code != http.StatusOK {
		t.Fatalf("claim: %d %s", code, body)
	}
	waitFor(t, "normal mode", time.Minute, func() (bool, string) {
		s := rbdStatus(t)
		return s.Mode == "normal", s.Mode
	})
	p.join(t, hotspotSSID, hotspotPassword, false)
	t.Logf("phone1 is on the hotspot with %s", p.connected(t))
	// The simulated radios can run Wi-Fi 6, so the hotspot does by itself
	// (and the phone still joined).
	if got := rbdStatus(t).Hotspot.Standard; got != "wifi6" {
		t.Errorf("the hotspot runs %q, want wifi6", got)
	}
}

// 2. RasBeacon joins the hotel's Wi-Fi, notices its login page, and the
// owner accepts the terms in RasBeacon's own browser. Then the phone
// reaches the internet, coming from RasBeacon's hotel address.
func hotelLogin(t *testing.T) {
	var settings map[string]any
	rbd(t, "GET", "/settings/uplink", nil, &settings)
	settings["networks"] = []map[string]any{{"id": "", "ssid": hotelSSID, "security": "open"}}
	rbd(t, "PUT", "/settings/uplink", settings, nil)

	waitFor(t, "the uplink and the login page", 2*time.Minute, func() (bool, string) {
		s := rbdStatus(t)
		if s.Uplink.Connected && !s.Portal.Detected {
			tryRBD("POST", "/portal/check", nil, nil)
		}
		return s.Uplink.Connected && s.Portal.Detected, fmt.Sprintf("connected %v, login page %v", s.Uplink.Connected, s.Portal.Detected)
	})

	rbd(t, "POST", "/portal/browser/start", map[string]int{"width": 400, "height": 600}, nil)
	waitFor(t, "the login page in the browser", time.Minute, func() (bool, string) {
		var frame struct{ URL, Title string }
		tryRBD("GET", "/portal/browser/frame?after=0", nil, &frame)
		return strings.Contains(frame.URL, hotelLAN), frame.URL + " " + frame.Title
	})
	// On a failure, show what the hotel saw.
	t.Cleanup(func() {
		if t.Failed() {
			out, _ := tryRun(hotelNS, "nft", "list", "set", "inet", "hotel", "authorized")
			log, _ := os.ReadFile(filepath.Join(stateDir, "hotel.log"))
			t.Logf("the hotel's authorized visitors:\n%s\nits web servers' log:\n%s", out, log)
		}
	})
	// The page is one big button: a click in the middle accepts.
	rbd(t, "POST", "/portal/browser/input", map[string]any{"action": "click", "x": 200, "y": 300}, nil)
	waitFor(t, "the hotel's welcome page", time.Minute, func() (bool, string) {
		var frame struct{ URL, Title string }
		tryRBD("GET", "/portal/browser/frame?after=0", nil, &frame)
		return frame.Title == "Connected", frame.URL + " " + frame.Title
	})
	waitFor(t, "the login page to go away", time.Minute, func() (bool, string) {
		tryRBD("POST", "/portal/check", nil, nil)
		s := rbdStatus(t)
		return !s.Portal.Detected, "still detected"
	})
	tryRBD("POST", "/portal/browser/stop", nil, nil)

	hotelAddress, _, _ := strings.Cut(rbdStatus(t).Uplink.Address, "/")
	waitFor(t, "phone1 to reach the internet", time.Minute, func() (bool, string) {
		code, body := phones[0].fetch("GET", "http://"+internetIP+"/whoami", "")
		return code == http.StatusOK && body == hotelAddress, fmt.Sprintf("%d %s (RasBeacon's hotel address is %s)", code, body, hotelAddress)
	})
}

// 3. With the VPN on, the internet sees the phone's requests come from the
// tunnel. When the VPN server stops, the kill switch keeps the phone off
// the internet entirely: nothing reaches it through the hotel either.
func vpnAndKillSwitch(t *testing.T) {
	conf, err := os.ReadFile(filepath.Join(stateDir, "vpn.conf"))
	if err != nil {
		t.Fatal(err)
	}
	rbd(t, "POST", "/settings/vpn/import", map[string]string{"conf": string(conf), "name": "test.conf"}, nil)
	rbd(t, "PUT", "/settings/vpn", map[string]bool{"enabled": true, "killSwitch": true}, nil)
	waitFor(t, "the tunnel", 2*time.Minute, func() (bool, string) {
		s := rbdStatus(t)
		return s.VPN.State == "up", s.VPN.State
	})
	waitFor(t, "phone1 through the tunnel", time.Minute, func() (bool, string) {
		code, body := phones[0].fetch("GET", "http://"+internetIP+"/whoami", "")
		return code == http.StatusOK && body == vpnClientIP, fmt.Sprintf("%d %s", code, body)
	})

	run(t, hotelNS, "ip", "link", "set", "wgs", "down")
	for range 3 {
		if code, body := phones[0].fetch("GET", "http://"+internetIP+"/whoami", ""); code != 0 {
			t.Fatalf("with the VPN server down the phone still got an answer: %d %s", code, body)
		}
	}
	run(t, hotelNS, "ip", "link", "set", "wgs", "up")
	waitFor(t, "phone1 through the tunnel again", 3*time.Minute, func() (bool, string) {
		code, body := phones[0].fetch("GET", "http://"+internetIP+"/whoami", "")
		return code == http.StatusOK && body == vpnClientIP, fmt.Sprintf("%d %s", code, body)
	})
}

// 4. A guest phone gets the internet, but neither RasBeacon's settings
// page nor the main hotspot's devices.
func guests(t *testing.T) {
	rbd(t, "PUT", "/settings/guest", map[string]any{"enabled": true, "ssid": guestSSID, "password": guestPassword}, nil)
	guest := phones[1]
	guest.join(t, guestSSID, guestPassword, false)
	address := guest.connected(t)
	// The guest network has its own addresses (10.87.1.0/24 by default),
	// apart from the main hotspot's (10.87.0.0/24).
	if strings.HasPrefix(address, "10.87.0.") {
		t.Errorf("the guest got %s, an address on the main hotspot's network", address)
	}
	guestGateway := address[:strings.LastIndex(address, ".")] + ".1"
	waitFor(t, "the guest to reach the internet", time.Minute, func() (bool, string) {
		code, body := guest.fetch("GET", "http://"+internetIP+"/whoami", "")
		return code == http.StatusOK, fmt.Sprintf("%d %s", code, body)
	})
	if code, body := guest.fetch("GET", "http://10.87.0.1/api/v1/setup", ""); code != 0 {
		t.Errorf("a guest reached the settings page: %d %s", code, body)
	}
	if code, body := guest.fetch("GET", "http://"+guestGateway+"/api/v1/setup", ""); code != 0 {
		t.Errorf("a guest reached the settings page on its own network: %d %s", code, body)
	}
	phone1, _ := tryRun(phones[0].ns, "ip", "-4", "-o", "address", "show", "dev", phones[0].iface)
	if ip := regexp.MustCompile(`inet (\S+)/`).FindStringSubmatch(phone1); ip != nil {
		if out, err := tryRun(guest.ns, "busybox", "ping", "-c", "2", "-W", "2", ip[1]); err == nil {
			t.Errorf("a guest reached a device on the main hotspot: %s", out)
		}
	}
}

// 5. With the allow list on, a phone that isn't listed can't join.
func allowList(t *testing.T) {
	listed := []string{phones[0].mac(t), phones[1].mac(t)}
	rbd(t, "PUT", "/settings/access", map[string]any{"allowListOn": true, "allowed": listed, "maxDevices": 0}, nil)
	stranger := phones[2]
	stranger.join(t, hotspotSSID, hotspotPassword, false)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := stranger.associated(); ok {
			t.Fatal("a phone that isn't on the allow list joined")
		}
		time.Sleep(time.Second)
	}
	stranger.leave()
	// The listed phone is still welcome (it rejoins after the restart).
	phones[0].connected(t)
	rbd(t, "PUT", "/settings/access", map[string]any{"allowListOn": false, "allowed": listed, "maxDevices": 0}, nil)
}

// 6. With Security set to WPA3 only, a phone joins with WPA3 (SAE and
// protected management frames).
func wpa3Only(t *testing.T) {
	var settings map[string]any
	rbd(t, "GET", "/settings/hotspot", nil, &settings)
	settings["security"] = "wpa3"
	settings["password"] = ""
	rbd(t, "PUT", "/settings/hotspot", settings, nil)
	p := phones[2]
	p.join(t, hotspotSSID, hotspotPassword, true)
	t.Logf("phone3 joined with WPA3 and got %s", p.connected(t))
	_, out := p.associated()
	if !strings.Contains(out, "key_mgmt=SAE") {
		t.Errorf("phone3 isn't using WPA3: %s", out)
	}
}

// 7. rbd restarts while the guest network runs: it must find its radios
// again as they were, and not take guest0 (a second interface of the
// hotspot's radio) for the uplink.
func restartWithGuests(t *testing.T) {
	run(t, "", "systemctl", "restart", "rbd.service")
	waitFor(t, "rbd and the uplink after the restart", 2*time.Minute, func() (bool, string) {
		var s status
		if err := tryRBD("GET", "/status", nil, &s); err != nil {
			return false, err.Error()
		}
		return s.Uplink.Connected, fmt.Sprintf("connected %v on %q", s.Uplink.Connected, s.Uplink.Interface)
	})
	if s := rbdStatus(t); s.Uplink.Interface == "guest0" || s.Uplink.Interface == "ap0" {
		t.Errorf("the uplink is %s, one of the hotspot's own interfaces", s.Uplink.Interface)
	}
}

// 8. The hotspot's interface taken out of its bridge (as some drivers do
// after an error) is put back by rbd's self-check within its interval.
func selfCheck(t *testing.T) {
	run(t, "", "ip", "link", "set", "ap0", "nomaster")
	waitFor(t, "ap0 back in br-lan", 3*time.Minute, func() (bool, string) {
		out, _ := tryRun("", "ip", "-o", "link", "show", "ap0")
		return strings.Contains(out, "master br-lan"), strings.TrimSpace(out)
	})
}

// 9. With a cloned MAC address, RasBeacon joins the hotel as that device:
// the hotel's DHCP server hands the address to the cloned MAC.
func clonedMAC(t *testing.T) {
	const cloned = "02:52:42:00:00:99"
	var settings map[string]any
	rbd(t, "GET", "/settings/uplink", nil, &settings)
	settings["wifiMac"] = map[string]any{"mode": "cloned", "address": cloned}
	rbd(t, "PUT", "/settings/uplink", settings, nil)
	waitFor(t, "a hotel lease for the cloned address", 2*time.Minute, func() (bool, string) {
		leases, _ := os.ReadFile(filepath.Join(stateDir, "hotel.leases"))
		return strings.Contains(string(leases), cloned), strings.TrimSpace(string(leases))
	})
	out, _ := tryRun("", "ip", "-o", "link", "show", "wlan1")
	if !strings.Contains(out, cloned) {
		t.Errorf("wlan1 should have the cloned address: %s", out)
	}
}
