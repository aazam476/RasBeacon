package clock

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgreedTime(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	floor := base.Add(-24 * time.Hour)
	tests := []struct {
		name    string
		samples []time.Time
		want    time.Time
		ok      bool
	}{
		{"two agree", []time.Time{base, base.Add(2 * time.Second)}, base.Add(2 * time.Second), true},
		{"one wrong, two agree", []time.Time{base.Add(-90 * 24 * time.Hour), base, base.Add(time.Second)}, base.Add(time.Second), true},
		{"only one answer", []time.Time{base}, time.Time{}, false},
		{"none agree", []time.Time{base, base.Add(time.Minute), base.Add(2 * time.Minute)}, time.Time{}, false},
		{"before the floor", []time.Time{floor.Add(-time.Hour), floor.Add(-time.Hour)}, time.Time{}, false},
		{"failed requests", []time.Time{{}, {}, base}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AgreedTime(tt.samples, floor)
			if ok != tt.ok || !got.Equal(tt.want) {
				t.Errorf("got %v %v, want %v %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFetchDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Thu, 01 Oct 2026 12:00:00 GMT")
	}))
	defer server.Close()
	got, err := FetchDate(context.Background(), server.Client(), server.URL)
	if err != nil || !got.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("got %v, %v", got, err)
	}
}

// testCertificates makes a certificate authority and a server certificate
// it signed, for names, valid only during 2015.
func testCertificates(t *testing.T, names []string) (*x509.CertPool, tls.Certificate) {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	notBefore := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	caKey := newKey()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: notBefore, NotAfter: notBefore.AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	serverKey := newKey()
	serverDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2), NotBefore: notBefore.AddDate(0, 1, 0), NotAfter: notBefore.AddDate(0, 6, 0),
		DNSNames: names, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return roots, tls.Certificate{Certificate: [][]byte{serverDER, caDER}, PrivateKey: serverKey}
}

// dateServer is an HTTPS server with certificate cert that says it is
// 1 October 2026. It returns its address as https://localhost:<port>, as
// the date sources are reached by name.
func dateServer(t *testing.T, cert tls.Certificate) string {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Thu, 01 Oct 2026 12:00:00 GMT")
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
}

// The certificate is checked, except for its dates: a clock years off
// must still be able to ask for the time.
func TestClient_ChecksCertificatesButNotTheirDates(t *testing.T) {
	roots, cert := testCertificates(t, []string{"localhost"})
	server := dateServer(t, cert)

	got, err := FetchDate(context.Background(), Client(roots), server)
	if err != nil || !got.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("a certificate that expired in 2015: got %v, %v; want the server's time", got, err)
	}

	// Signed by an authority it doesn't know: refused.
	otherRoots, _ := testCertificates(t, []string{"localhost"})
	if _, err := FetchDate(context.Background(), Client(otherRoots), server); err == nil {
		t.Error("a certificate from an unknown authority must be refused")
	}

	// Reached by address instead of name: there is no name to check.
	if _, err := FetchDate(context.Background(), Client(roots), strings.Replace(server, "localhost", "127.0.0.1", 1)); err == nil {
		t.Error("a server reached by its address can't be checked, so it must be refused")
	}

	// A certificate for another name: refused.
	roots, cert = testCertificates(t, []string{"www.example.com"})
	server = dateServer(t, cert)
	if _, err := FetchDate(context.Background(), Client(roots), server); err == nil {
		t.Error("a certificate for another name must be refused")
	}
}

func TestDateSources_AreHTTPS(t *testing.T) {
	for _, source := range DateSources {
		if !strings.HasPrefix(source, "https://") {
			t.Errorf("%s: a network with a login page could answer a plain-HTTP request with any time", source)
		}
	}
}
