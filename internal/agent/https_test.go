package agent

import (
	"errors"
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
