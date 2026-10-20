package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"strings"
	"testing"
	"time"
)

func TestGenerate_UsableForTheHotspotAddresses(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	certPEM, keyPEM, err := Generate("RasBeacon 4F2A", []string{"travel-pi.lan"}, []net.IP{net.ParseIP("10.87.0.1"), net.ParseIP("10.88.0.1")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(certPEM, keyPEM, now); err != nil {
		t.Fatalf("a generated certificate must pass Check: %v", err)
	}
	pair, _ := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	for _, host := range []string{"10.87.0.1", "10.88.0.1", "rasbeacon.lan", "travel-pi.lan"} {
		if err := leaf.VerifyHostname(host); err != nil {
			t.Errorf("not valid for %s: %v", host, err)
		}
	}
	info, err := Describe(certPEM)
	if err != nil || info.Subject != "RasBeacon 4F2A" || len(info.Fingerprint) != 95 {
		t.Errorf("info = %+v, %v", info, err)
	}
}

func TestCheck_RejectsMismatchedOrExpired(t *testing.T) {
	now := time.Now()
	certA, _, _ := Generate("a", nil, nil, now)
	_, keyB, _ := Generate("b", nil, nil, now)
	if err := Check(certA, keyB, now); err == nil {
		t.Error("a key from another certificate must be rejected")
	}
	cert, key, _ := Generate("old", nil, nil, now.Add(-11*365*24*time.Hour))
	if err := Check(cert, key, now); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("an expired certificate must be rejected, got %v", err)
	}
	encrypted := string(pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{1}}))
	if err := Check(certA, encrypted, now); err == nil || !strings.Contains(err.Error(), "password-protected") {
		t.Errorf("got %v", err)
	}
}
