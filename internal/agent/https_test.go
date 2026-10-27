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
	"path/filepath"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/tlscert"
)

func TestHTTPS_TurningOnMakesACertificateForRbweb(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s := e.agent.HTTPSSettings()
	if !s.Enabled || s.Uploaded || s.Fingerprint == "" || s.Subject == "" {
		t.Fatalf("settings = %+v", s)
	}
	runKey := filepath.Join(e.paths.RunTLSDir(), "key.pem")
	info, err := os.Stat(runKey)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("rbweb's copy of the key should exist with mode 640: %v", err)
	}
	keyInfo, _ := os.Stat(filepath.Join(e.paths.TLSDir(), "key.pem"))
	if keyInfo.Mode().Perm() != 0o600 {
		t.Error("the stored key must be root-only")
	}
	e.timers.runAll()
	if !e.runner.ran("systemctl restart rbweb.service") {
		t.Error("rbweb should restart to pick up HTTPS")
	}

	// Turning it off removes rbweb's copy but keeps the certificate.
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.paths.RunTLSDir()); !os.IsNotExist(err) {
		t.Error("rbweb's copy should be removed when HTTPS is off")
	}
	if e.agent.HTTPSSettings().Fingerprint != s.Fingerprint {
		t.Error("the certificate should be kept for next time")
	}
}

func TestHTTPS_UploadIsChecked(t *testing.T) {
	e := newClaimedEnv(t, false)
	certA, _, _ := tlscert.Generate("mine", nil, nil, time.Now())
	_, keyB, _ := tlscert.Generate("other", nil, nil, time.Now())
	err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true, Certificate: certA, PrivateKey: keyB})
	var verr *config.ValidationError
	if !errors.As(err, &verr) || verr.Field != "certificate" {
		t.Fatalf("got %v", err)
	}

	certA2, keyA2, _ := tlscert.Generate("mine", nil, nil, time.Now())
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true, Certificate: certA2, PrivateKey: keyA2}); err != nil {
		t.Fatal(err)
	}
	if s := e.agent.HTTPSSettings(); !s.Uploaded || s.Subject != "mine" {
		t.Errorf("settings = %+v", s)
	}
}

// A certificate RasBeacon made is replaced before it runs out, and rbweb
// restarts with the new one. An uploaded one is left to the owner.
func TestHTTPS_GeneratedCertificateIsRenewedBeforeItRunsOut(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	old := e.agent.HTTPSSettings().Fingerprint

	e.agent.mu.Lock()
	e.agent.renewCertificateIfDue() // a new certificate: nothing to do
	e.agent.mu.Unlock()
	if e.agent.HTTPSSettings().Fingerprint != old {
		t.Fatal("a new certificate was replaced")
	}

	// 800 days later, it runs out within a month.
	later := time.Now().Add(800 * 24 * time.Hour)
	e.agent.now = func() time.Time { return later }
	e.runner.commands = nil
	e.agent.mu.Lock()
	e.agent.renewCertificateIfDue()
	e.agent.mu.Unlock()
	if e.agent.HTTPSSettings().Fingerprint == old {
		t.Error("the certificate should be renewed")
	}
	if !e.runner.ran("systemctl restart rbweb.service") {
		t.Error("rbweb restarts with the new certificate")
	}
	data, _ := os.ReadFile(filepath.Join(e.paths.RunTLSDir(), "cert.pem"))
	if info, err := tlscert.Describe(string(data)); err != nil || info.Fingerprint != e.agent.HTTPSSettings().Fingerprint {
		t.Errorf("rbweb's copy should be the new certificate: %v", err)
	}

	// An uploaded certificate is never replaced.
	e.agent.mu.Lock()
	e.agent.cfg.HTTPS.Uploaded = true
	e.agent.lastCertCheck = time.Time{}
	e.agent.now = func() time.Time { return later.Add(900 * 24 * time.Hour) }
	renewed := e.agent.HTTPSSettings
	e.agent.mu.Unlock()
	before := renewed().Fingerprint
	e.agent.mu.Lock()
	e.agent.renewCertificateIfDue()
	e.agent.mu.Unlock()
	if renewed().Fingerprint != before {
		t.Error("an uploaded certificate is the owner's to replace")
	}
}

// RasBeacon 0.1 made certificates valid for ten years, which Apple's
// devices may refuse: the next start replaces them.
func TestHTTPS_TenYearCertificateIsReplacedAtStart(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// A certificate like 0.1's: made a year ago, valid for ten years.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	made := time.Now().AddDate(-1, 0, 0)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RasBeacon 0.1"},
		NotBefore: made, NotAfter: made.AddDate(10, 0, 0), DNSNames: []string{"rasbeacon.lan"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	mustWrite(t, e.agent.tlsCertFile(), string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	mustWrite(t, e.agent.tlsKeyFile(), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})))

	restarted := New(e.paths, e.runner)
	restarted.afterFunc, restarted.usbRadioWait, restarted.background = e.timers.afterFunc, 0, func(func()) {}
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	s := restarted.HTTPSSettings()
	if s.Subject == "RasBeacon 0.1" || !s.Enabled {
		t.Errorf("after the start: %+v, want a new certificate", s)
	}
}

func TestHTTPS_RbwebIsRestartedAfterABootToFindTheCertificate(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// A reboot: /run is empty again, and rbweb may already have started
	// without the certificate.
	if err := os.RemoveAll(e.paths.RunTLSDir()); err != nil {
		t.Fatal(err)
	}
	e.runner.commands = nil
	restarted := New(e.paths, e.runner)
	restarted.afterFunc, restarted.usbRadioWait, restarted.background = e.timers.afterFunc, 0, func(func()) {}
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	if !e.runner.ran("systemctl restart rbweb.service") {
		t.Errorf("rbweb should be restarted to serve HTTPS; commands: %v", e.runner.commands)
	}
	if !fileExists(filepath.Join(e.paths.RunTLSDir(), "cert.pem")) {
		t.Error("rbweb's copy of the certificate should be back")
	}

	// A start where the copy is already there (rbd restarted alone) leaves
	// rbweb alone.
	e.runner.commands = nil
	again := New(e.paths, e.runner)
	again.afterFunc, again.usbRadioWait, again.background = e.timers.afterFunc, 0, func(func()) {}
	if err := again.Start(); err != nil {
		t.Fatal(err)
	}
	if e.runner.ran("systemctl restart rbweb.service") {
		t.Error("rbweb must not be restarted when nothing changed")
	}
}

// rbweb runs as its own user: the certificate and key it reads are given
// to its group (root keeps owning them), and nobody else may read them.
func TestHTTPS_FilesAreGivenToRbwebsGroup(t *testing.T) {
	e := newClaimedEnv(t, false)
	// rbd makes root the files' owner, which only root may do, so this
	// runs only as root. The test's own group stands in for rbweb's.
	if os.Getuid() != 0 {
		t.Skip("giving files to root needs root")
	}
	e.agent.SetWebGroup(os.Getgid())
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cert.pem", "key.pem"} {
		info, err := os.Stat(filepath.Join(e.paths.RunTLSDir(), name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Errorf("%s mode = %v, want 0640", name, info.Mode().Perm())
		}
	}
}

// HTTPS is on but its files are gone (a damaged card): rbd says what is
// missing instead of serving nothing.
func TestHTTPS_MissingCertificateFile(t *testing.T) {
	e := newClaimedEnv(t, false)
	if err := e.agent.UpdateHTTPSSettings(api.HTTPSSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	os.Remove(e.agent.tlsKeyFile())
	if _, err := e.agent.applyHTTPS(); err == nil {
		t.Error("a missing key must be an error")
	}
	os.Remove(e.agent.tlsCertFile())
	if _, err := e.agent.applyHTTPS(); err == nil {
		t.Error("a missing certificate must be an error")
	}
}
