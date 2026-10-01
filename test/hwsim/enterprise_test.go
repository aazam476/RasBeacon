// Package hwsim tests RasBeacon's WPA-Enterprise configuration against a
// real Wi-Fi login, using simulated radios.
//
// # What is simulated
//
// The Linux module mac80211_hwsim creates virtual Wi-Fi radios that talk to
// each other over the air, inside the computer. One radio runs hostapd as a
// campus network ("campus") with hostapd's built-in login server (EAP
// server), standing in for eduroam's RADIUS servers. The other runs
// wpa_supplicant with the exact configuration RasBeacon generates.
//
// Each test checks one login method, or that RasBeacon refuses a login
// server it should not trust.
//
// # Running
//
// Needs root, the mac80211_hwsim module, hostapd and wpa_supplicant.
// Without them the tests are skipped, unless RASBEACON_REQUIRE_HWSIM=1 is
// set (as CI does), in which case they fail.
//
//	sudo modprobe mac80211_hwsim radios=2
//	sudo -E go test ./test/hwsim/ -v
package hwsim

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
	"rasbeacon/internal/uplink"
)

// The campus network's login details.
const (
	campusSSID     = "campus"
	serverName     = "radius.campus.test"
	userIdentity   = "alice@campus.test"
	userPassword   = "alice-password"
	wpaControlDir  = "/run/wpa_supplicant" // fixed in RasBeacon's template
	connectTimeout = 25 * time.Second
	refuseWait     = 12 * time.Second
)

// requireHwsim returns the two simulated radios, or skips the test.
func requireHwsim(t *testing.T) (apInterface, staInterface string) {
	t.Helper()
	reason := ""
	if os.Geteuid() != 0 {
		reason = "needs root"
	}
	for _, tool := range []string{"hostapd", "wpa_supplicant", "wpa_cli"} {
		if _, err := exec.LookPath(tool); err != nil && reason == "" {
			reason = "missing " + tool
		}
	}
	radios := hwsimInterfaces()
	if reason == "" && len(radios) < 2 {
		reason = "needs two mac80211_hwsim radios (sudo modprobe mac80211_hwsim radios=2)"
	}
	if reason != "" {
		if os.Getenv("RASBEACON_REQUIRE_HWSIM") == "1" {
			t.Fatalf("hwsim tests cannot run: %s", reason)
		}
		t.Skipf("skipping hwsim tests: %s", reason)
	}
	return radios[0], radios[1]
}

// hwsimInterfaces lists the network interfaces of simulated radios.
func hwsimInterfaces() []string {
	var names []string
	entries, _ := os.ReadDir("/sys/class/net")
	for _, entry := range entries {
		driver, err := os.Readlink(filepath.Join("/sys/class/net", entry.Name(), "device", "driver"))
		if err == nil && filepath.Base(driver) == "mac80211_hwsim" {
			names = append(names, entry.Name())
		}
	}
	return names
}

// --- A small certificate authority for the campus network ---

type keyPair struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM string
	keyPEM  string
}

// newCA creates a certificate authority. A second, unrelated CA stands in
// for an attacker's.
func newCA(t *testing.T, name string) keyPair {
	t.Helper()
	return issue(t, name, nil, &x509.Certificate{IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature})
}

// issue creates a certificate signed by ca (or self-signed if ca is nil).
func issue(t *testing.T, name string, ca *keyPair, template *x509.Certificate) keyPair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	template.SerialNumber = serial
	template.Subject = pkix.Name{CommonName: name}
	template.NotBefore = time.Now().Add(-time.Hour)
	template.NotAfter = time.Now().Add(24 * time.Hour)
	parent, signer := template, key
	if ca != nil {
		parent, signer = ca.cert, ca.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	return keyPair{
		cert: cert, key: key,
		certPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		keyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}
}

// campus holds the files of a running campus network.
type campus struct {
	dir          string
	caFile       string // the real campus CA, for RasBeacon
	clientCert   string // alice's certificate, for EAP-TLS
	clientKey    string
	rogueCAFile  string // an unrelated CA
	staInterface string
}

// startCampus runs hostapd as the campus network on apInterface.
func startCampus(t *testing.T, apInterface, staInterface string) campus {
	t.Helper()
	dir := t.TempDir()
	ca := newCA(t, "Campus Test Root CA")
	server := issue(t, serverName, &ca, &x509.Certificate{DNSNames: []string{serverName},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	client := issue(t, userIdentity, &ca, &x509.Certificate{
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	rogue := newCA(t, "Somebody Else's CA")

	c := campus{dir: dir, staInterface: staInterface}
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	c.caFile = write("ca.pem", ca.certPEM)
	c.clientCert = write("client.pem", client.certPEM)
	c.clientKey = write("client-key.pem", client.keyPEM)
	c.rogueCAFile = write("rogue-ca.pem", rogue.certPEM)

	// Who may log in, and how. "*" accepts any outer (anonymous) identity
	// for the tunnelled methods; the [2] line is checked inside the tunnel.
	// hostapd uses the first line that matches a name, so all of alice's
	// inner methods must be on one line.
	users := write("eap_users", "*\tTTLS,PEAP,TLS\n"+
		`"`+userIdentity+`"`+"\tTTLS-PAP,MSCHAPV2\t\""+userPassword+"\"\t[2]\n")
	conf := write("hostapd.conf", strings.Join([]string{
		"interface=" + apInterface,
		"driver=nl80211",
		"ssid=" + campusSSID,
		"hw_mode=g",
		"channel=1",
		"wpa=2",
		"wpa_key_mgmt=WPA-EAP WPA-EAP-SHA256",
		"rsn_pairwise=CCMP",
		"ieee80211w=1",
		"ieee8021x=1",
		"eap_server=1",
		"eap_user_file=" + users,
		"ca_cert=" + c.caFile,
		"server_cert=" + write("server.pem", server.certPEM),
		"private_key=" + write("server-key.pem", server.keyPEM),
		"",
	}, "\n"))

	cmd := exec.Command("hostapd", conf)
	log, _ := os.Create(filepath.Join(dir, "hostapd.log"))
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting hostapd: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Logf("hostapd log:\n%s", tail(string(data), 40))
		}
	})
	time.Sleep(2 * time.Second) // let the network start beaconing
	return c
}

// loginResult is how a login attempt ended.
type loginResult struct {
	status uplink.WiFiStatus
	// attempted is true if the login server was reached and the login
	// actually ran (and failed or succeeded), as opposed to never finding
	// the network.
	attempted bool
	// paused is true if wpa_supplicant paused the network after a failure,
	// which is what RasBeacon shows as "the network rejected the login".
	paused bool
}

// login runs wpa_supplicant with RasBeacon's configuration for one
// Enterprise network and reports how it ended: connected, or not within
// wait.
func (c campus) login(t *testing.T, network render.WPANetwork, wait time.Duration) loginResult {
	t.Helper()
	conf, err := render.WPASupplicant(render.WPAInput{Country: "US", Networks: []render.WPANetwork{network}})
	if err != nil {
		t.Fatalf("rendering wpa_supplicant.conf: %v", err)
	}
	confFile := filepath.Join(t.TempDir(), "wpa_supplicant.conf")
	if err := os.WriteFile(confFile, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("wpa_supplicant", "-i", c.staInterface, "-c", confFile, "-D", "nl80211")
	logFile := filepath.Join(t.TempDir(), "wpa.log")
	log, _ := os.Create(logFile)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting wpa_supplicant: %v", err)
	}
	defer c.stopSupplicant(cmd)

	var result loginResult
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("wpa_cli", "-p", wpaControlDir, "-i", c.staInterface, "status").Output()
		result.status = uplink.ParseWPAStatus(string(out))
		if result.status.Connected() {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	networks, _ := exec.Command("wpa_cli", "-p", wpaControlDir, "-i", c.staInterface, "list_networks").Output()
	result.paused = len(uplink.ParseTempDisabled(string(networks))) > 0

	data, _ := os.ReadFile(logFile)
	logText := string(data)
	result.attempted = strings.Contains(logText, "CTRL-EVENT-EAP-SUCCESS") || strings.Contains(logText, "CTRL-EVENT-EAP-FAILURE")
	if !result.status.Connected() {
		t.Logf("wpa_supplicant log (last lines):\n%s", tail(logText, 25))
	}
	return result
}

// stopSupplicant stops wpa_supplicant cleanly and resets the radio, so the
// next test starts fresh. (Killing it outright can leave the radio "busy"
// and unable to scan.)
func (c campus) stopSupplicant(cmd *exec.Cmd) {
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	exec.Command("ip", "link", "set", "dev", c.staInterface, "down").Run()
	exec.Command("ip", "link", "set", "dev", c.staInterface, "up").Run()
	time.Sleep(time.Second)
}

func tail(text string, lines int) string {
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// network builds the Enterprise network RasBeacon would configure.
func (c campus) network(method config.EAPMethod, password string) render.WPANetwork {
	n := render.WPANetwork{
		SSID: campusSSID, Security: config.UplinkEnterprise, Password: password,
		Enterprise: &render.WPAEnterprise{
			Method: method, Identity: userIdentity, AnonymousIdentity: "anonymous@campus.test",
			// The owner typed "*.campus.test", as institutions publish it.
			ServerDomain: config.EnterpriseConfig{ServerName: "*.campus.test"}.ServerDomain(),
			CACertFile:   c.caFile,
		},
	}
	if method == config.EAPTLS {
		n.Enterprise.ClientCertFile = c.clientCert
		n.Enterprise.ClientKeyFile = c.clientKey
		n.Password = "" // the test key has no passphrase
	}
	return n
}

func TestEnterpriseLogin(t *testing.T) {
	ap, sta := requireHwsim(t)
	c := startCampus(t, ap, sta)

	// Each method must log in.
	for _, method := range []config.EAPMethod{config.EAPTTLSPAP, config.EAPPEAP, config.EAPTLS} {
		t.Run(string(method), func(t *testing.T) {
			if r := c.login(t, c.network(method, userPassword), connectTimeout); !r.status.Connected() {
				t.Errorf("%s login should succeed, final state %+v", method, r.status)
			}
		})
	}

	// expectRefused checks that a login was attempted and rejected: the
	// device reached the network but did not connect, and the network is
	// paused (shown as "the network rejected the login").
	expectRefused := func(t *testing.T, r loginResult) {
		t.Helper()
		if r.status.Connected() {
			t.Fatal("connected, but this login must be refused")
		}
		if !r.attempted {
			t.Fatal("the login was never attempted (the network was not found), so this proves nothing")
		}
		if !r.paused {
			t.Error("wpa_supplicant should pause the network, which RasBeacon reports as a rejected login")
		}
	}

	// RasBeacon must refuse servers it should not trust, so a fake
	// "eduroam" can't collect the password.
	t.Run("wrong server name is refused", func(t *testing.T) {
		n := c.network(config.EAPTTLSPAP, userPassword)
		n.Enterprise.ServerDomain = "other.test"
		expectRefused(t, c.login(t, n, refuseWait))
	})
	t.Run("server signed by another CA is refused", func(t *testing.T) {
		n := c.network(config.EAPTTLSPAP, userPassword)
		n.Enterprise.CACertFile = c.rogueCAFile
		expectRefused(t, c.login(t, n, refuseWait))
	})
	t.Run("wrong password is refused", func(t *testing.T) {
		expectRefused(t, c.login(t, c.network(config.EAPPEAP, "wrong-password"), refuseWait))
	})
}
