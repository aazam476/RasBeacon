package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

// makeCertificate creates a self-signed certificate for tests.
func makeCertificate(t *testing.T, name string, notAfter time.Time) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

func TestParseCertificates_PEMBundleAndDER(t *testing.T) {
	future := time.Date(2038, 1, 18, 0, 0, 0, 0, time.UTC)
	a, _ := makeCertificate(t, "Test Root A", future)
	b, _ := makeCertificate(t, "Test Root B", future)
	bundle := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a})) +
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b}))

	out, parsed, err := ParseCertificates(bundle)
	if err != nil || len(parsed) != 2 || strings.Count(out, "BEGIN CERTIFICATE") != 2 {
		t.Fatalf("PEM bundle: %v, %d certificates", err, len(parsed))
	}
	_, parsed, err = ParseCertificates(base64.StdEncoding.EncodeToString(a))
	if err != nil || len(parsed) != 1 || parsed[0].Subject.CommonName != "Test Root A" {
		t.Fatalf("DER: %v", err)
	}
	if got := Summary(parsed, time.Now()); got != "Test Root A (valid until 2038-01-18)" {
		t.Errorf("Summary = %q", got)
	}
	if got := Summary(parsed, future.Add(time.Hour)); !strings.Contains(got, "EXPIRED") {
		t.Errorf("Summary of an expired certificate = %q", got)
	}
}

func TestParseCertificates_RejectsJunk(t *testing.T) {
	for _, upload := range []string{"", "hello", "-----BEGIN CERTIFICATE-----\nnope\n-----END CERTIFICATE-----\n",
		base64.StdEncoding.EncodeToString([]byte("not DER"))} {
		if _, _, err := ParseCertificates(upload); err == nil {
			t.Errorf("accepted %q", upload)
		}
	}
}

func TestParsePrivateKey(t *testing.T) {
	_, key := makeCertificate(t, "client", time.Now().Add(time.Hour))
	ecDER, _ := x509.MarshalECPrivateKey(key)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(key)

	for name, upload := range map[string]string{
		"EC PEM":    string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})),
		"PKCS8 PEM": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})),
		"EC DER":    base64.StdEncoding.EncodeToString(ecDER),
		"encrypted": "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----\n",
	} {
		out, err := ParsePrivateKey(upload)
		if err != nil || !strings.Contains(out, "PRIVATE KEY-----") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParsePrivateKey("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"); err == nil {
		t.Error("a certificate is not a key")
	}
}
