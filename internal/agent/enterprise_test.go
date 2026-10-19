package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// testCACert returns a PEM CA certificate named "Test Campus Root CA".
func testCACert(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "Test Campus Root CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func eduroamRequest(caCert, password string) api.UplinkSettings {
	return api.UplinkSettings{
		EthernetRole: config.EthernetUplink,
		Networks: []api.SavedNetwork{{
			SSID: "eduroam", Security: config.UplinkEnterprise, Password: password,
			Enterprise: &api.EnterpriseSettings{
				Method: config.EAPTTLSPAP, Identity: "NetID@rutgers.edu", AnonymousIdentity: "anonymous@rutgers.edu",
				ServerName: "*.ruw.rutgers.edu", CACert: caCert,
			},
		}},
	}
}

func TestEnterprise_ServerNameAloneUsesTheSystemsCAs(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateUplinkSettings(eduroamRequest("", "my-password")); err != nil {
		t.Fatal(err)
	}
	conf := e.readFile(e.paths.WPASupplicantConfig())
	if !strings.Contains(conf, `ca_cert="/etc/ssl/certs/ca-certificates.crt"`) || !strings.Contains(conf, `domain_suffix_match="ruw.rutgers.edu"`) {
		t.Errorf("wpa_supplicant.conf:\n%s", conf)
	}
}

func TestEnterprise_NeitherFirstFetchesTheServersCertificate(t *testing.T) {
	e := newClaimedEnv(t, false)
	req := eduroamRequest("", "my-password")
	req.Networks[0].Enterprise.ServerName = ""
	if err := e.agent.UpdateUplinkSettings(req); err != nil {
		t.Fatal(err)
	}
	if conf := e.readFile(e.paths.WPASupplicantConfig()); !strings.Contains(conf, `ca_cert="probe://"`) {
		t.Errorf("the first connection must only fetch the certificate:\n%s", conf)
	}
}

func TestEnterprise_SavesCertificateAndWritesConfig(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateUplinkSettings(eduroamRequest(testCACert(t), "my-password")); err != nil {
		t.Fatal(err)
	}

	settings := e.agent.UplinkSettings()
	n := settings.Networks[0]
	if n.Enterprise == nil || !strings.Contains(n.Enterprise.CACertSummary, "Test Campus Root CA (valid until 2040-01-01)") {
		t.Fatalf("certificate summary missing: %+v", n.Enterprise)
	}
	if n.Enterprise.CACert != "" || n.Password != "" {
		t.Error("certificates and passwords are write-only")
	}

	caFile := e.paths.CACertFile(n.ID)
	wpa := e.readFile(e.paths.WPASupplicantConfig())
	for _, want := range []string{"eap=TTLS", `ca_cert="` + caFile + `"`, `domain_suffix_match="ruw.rutgers.edu"`} {
		if !strings.Contains(wpa, want) {
			t.Errorf("wpa_supplicant.conf is missing %q", want)
		}
	}

	// Saving again without re-uploading keeps the certificate and password.
	n.Password = ""
	n.Enterprise.CACert = ""
	if err := e.agent.UpdateUplinkSettings(api.UplinkSettings{EthernetRole: config.EthernetUplink, Networks: []api.SavedNetwork{n}}); err != nil {
		t.Fatalf("saving again: %v", err)
	}
	if !strings.Contains(e.readFile(e.paths.WPASupplicantConfig()), "password=6d792d70617373776f7264") {
		t.Error("the saved password should be kept")
	}

	// Forgetting the network deletes its certificate.
	if err := e.agent.UpdateUplinkSettings(api.UplinkSettings{EthernetRole: config.EthernetUplink}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(caFile); !os.IsNotExist(err) {
		t.Error("the certificate of a forgotten network should be deleted")
	}
}

func TestEnterprise_BadCertificateIsRejectedWithField(t *testing.T) {
	e := newClaimedEnv(t, false)
	err := e.agent.UpdateUplinkSettings(eduroamRequest("not a certificate", "pw"))
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "networks.0.enterprise.caCert" {
		t.Fatalf("got %v", err)
	}
}

func TestEnterprise_NoCertificateCheckIsAllowedButWarned(t *testing.T) {
	e := newClaimedEnv(t, false)
	req := eduroamRequest("", "pw")
	req.Networks[0].Enterprise.AllowNoCertificateCheck = true
	if err := e.agent.UpdateUplinkSettings(req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(e.agent.Status().Warnings, " "), "does not check its login server") {
		t.Error("a warning should stay on the status page")
	}
	if strings.Contains(e.readFile(e.paths.WPASupplicantConfig()), "ca_cert=") {
		t.Error("no CA certificate should be configured")
	}
}

func TestDiagnostics_MasksEnterpriseUsername(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateUplinkSettings(eduroamRequest(testCACert(t), "my-password")); err != nil {
		t.Fatal(err)
	}
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 status"] =
		"wpa_state=COMPLETED\nssid=eduroam\nidentity=NetID@rutgers.edu\n"
	report := e.agent.Diagnostics()
	if strings.Contains(report, "NetID@rutgers.edu") || !strings.Contains(report, "N***@rutgers.edu") {
		t.Error("the username should appear only masked")
	}
	if strings.Contains(report, "my-password") {
		t.Error("the password must never appear")
	}
}

func TestMaskIdentity(t *testing.T) {
	for in, want := range map[string]string{
		"NetID@rutgers.edu": "N***@rutgers.edu",
		"alice":             "a***",
		"@realm":            "***@realm",
		"émile@x.fr":        "é***@x.fr",
	} {
		if got := MaskIdentity(in); got != want {
			t.Errorf("MaskIdentity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestObserveWiFi_ReportsFailedLogin(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateUplinkSettings(eduroamRequest(testCACert(t), "wrong")); err != nil {
		t.Fatal(err)
	}
	// What wpa_supplicant reports after a rejected login: not connected,
	// and the network paused.
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 status"] = "wpa_state=DISCONNECTED\n"
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 list_networks"] =
		"network id / ssid / bssid / flags\n0\teduroam\tany\t[TEMP-DISABLED]\n"
	e.check()
	wifi := e.agent.Status().Uplink.WiFi
	if wifi.State != "login-failed" || wifi.SSID != "eduroam" {
		t.Errorf("got %+v, want login-failed for eduroam", wifi)
	}
}

func TestEnterprise_TrustsTheServerSeenAtTheFirstConnection(t *testing.T) {
	e := newClaimedEnv(t, false)
	req := eduroamRequest("", "my-password")
	req.Networks[0].Enterprise.ServerName = ""
	if err := e.agent.UpdateUplinkSettings(req); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("5a", 32)
	// wpa_supplicant joins and reports the server's certificate.
	e.agent.handleWPAEvent("Trying to associate with 00:11:22:33:44:55 (SSID='eduroam' freq=2412 MHz)")
	e.agent.handleWPAEvent("CTRL-EVENT-EAP-PEER-CERT depth=1 subject='/CN=Example CA' hash=" + strings.Repeat("11", 32))
	e.agent.handleWPAEvent("CTRL-EVENT-EAP-PEER-CERT depth=0 subject='/CN=radius.example.com' hash=" + hash)

	conf := e.readFile(e.paths.WPASupplicantConfig())
	if !strings.Contains(conf, `ca_cert="hash://server/sha256/`+hash+`"`) || strings.Contains(conf, "probe://") {
		t.Fatalf("only the trusted certificate should be accepted now:\n%s", conf)
	}
	settings := e.agent.UplinkSettings()
	if got := settings.Networks[0].Enterprise.ServerCertHash; got != hash {
		t.Errorf("serverCertHash = %q", got)
	}

	// Another certificate later is not trusted instead.
	e.agent.handleWPAEvent("CTRL-EVENT-EAP-PEER-CERT depth=0 subject='/CN=evil' hash=" + strings.Repeat("ee", 32))
	e.agent.handleWPAEvent("CTRL-EVENT-EAP-TLS-CERT-ERROR reason=8 depth=0 subject='/CN=evil' err='Server certificate mismatch'")
	if got := e.agent.UplinkSettings().Networks[0].Enterprise.ServerCertHash; got != hash {
		t.Errorf("the trusted certificate must not change: %q", got)
	}
	if warnings := strings.Join(e.agent.Status().Warnings, "\n"); !strings.Contains(warnings, "different certificate") {
		t.Errorf("warnings = %q", warnings)
	}

	// Saving the list again keeps it.
	again := e.agent.UplinkSettings()
	if err := e.agent.UpdateUplinkSettings(again); err != nil {
		t.Fatal(err)
	}
	if got := e.agent.UplinkSettings().Networks[0].Enterprise.ServerCertHash; got != hash {
		t.Errorf("saving the list again forgot the trusted certificate: %q", got)
	}
}
