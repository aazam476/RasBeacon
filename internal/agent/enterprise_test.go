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

func TestEnterprise_RefusesToSaveWithoutCACertificate(t *testing.T) {
	e := newClaimedEnv(t, false)
	err := e.agent.UpdateUplinkSettings(eduroamRequest("", "my-password"))
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "networks.0.enterprise.caCert" {
		t.Fatalf("got %v, want a CA certificate error", err)
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
	e.runner.outputs["wpa_cli -p "+e.paths.WPAControlDir()+" -i wlan0 status"] =
		"wpa_state=ASSOCIATED\nssid=eduroam\nSupplicant PAE state=HELD\n"
	e.check()
	if got := e.agent.Status().Uplink.WiFi.State; got != "login-failed" {
		t.Errorf("state = %q, want login-failed", got)
	}
}
