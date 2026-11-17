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
		{"failed requests", []time.Time{{}, {}, base}, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AgreedTime(tt.samples)
			if ok != tt.ok || !got.Equal(tt.want) {
				t.Errorf("got %v %v, want %v %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFetchDate(t *testing.T) {
	answer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Thu, 01 Oct 2026 12:00:00 GMT")
	})
	server := httptest.NewTLSServer(answer)
	defer server.Close()
	got, err := FetchDate(context.Background(), server.Client(), server.URL)
	if err != nil || !got.Equal(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("got %v, %v", got, err)
	}

	// Over plain HTTP anyone on the way can answer: no time from there,
	// also not when an HTTPS server sends the request on to it.
	plain := httptest.NewServer(answer)
	defer plain.Close()
	if _, err := FetchDate(context.Background(), plain.Client(), plain.URL); err == nil {
		t.Error("a time over plain HTTP must be refused")
	}
	redirecting := httptest.NewTLSServer(http.RedirectHandler(plain.URL, http.StatusFound))
	defer redirecting.Close()
	if _, err := FetchDate(context.Background(), redirecting.Client(), redirecting.URL); err == nil {
		t.Error("a time from a plain-HTTP server an HTTPS server redirected to must be refused")
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
// date (a "Date" header's text). It returns its address as
// https://localhost:<port>, as the date sources are reached by name.
func dateServer(t *testing.T, cert tls.Certificate, date string) string {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", date)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
}

// The certificate is checked, but not against this computer's clock: a
// clock years off must still be able to ask for the time. Here the clock
// is the test's (today), the certificate is valid only during 2015, and
// the server says it is March 2015.
func TestClient_ChecksCertificatesButNotByTheClock(t *testing.T) {
	const march2015 = "Sun, 01 Mar 2015 12:00:00 GMT"
	roots, cert := testCertificates(t, []string{"localhost"})
	server := dateServer(t, cert, march2015)

	got, err := FetchDate(context.Background(), Client(roots), server)
	if err != nil || !got.Equal(time.Date(2015, 3, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("a certificate that this clock takes for expired: got %v, %v; want the server's time", got, err)
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
	otherName, otherCert := testCertificates(t, []string{"www.example.com"})
	if _, err := FetchDate(context.Background(), Client(otherName), dateServer(t, otherCert, march2015)); err == nil {
		t.Error("a certificate for another name must be refused")
	}
}

// The time a server gives must be one at which its certificate is valid.
// Whoever holds an old certificate that got out, with its key, can't say
// that it is today with it, or any time after it expired.
func TestFetchDate_TheTimeIsOneTheCertificateIsValidAt(t *testing.T) {
	roots, cert := testCertificates(t, []string{"localhost"}) // valid February to July 2015
	for _, date := range []string{
		"Thu, 01 Oct 2026 12:00:00 GMT", // long after it expired
		"Sat, 01 Aug 2015 00:00:00 GMT", // a month after
		"Thu, 15 Jan 2015 00:00:00 GMT", // before it was valid
	} {
		if got, err := FetchDate(context.Background(), Client(roots), dateServer(t, cert, date)); err == nil {
			t.Errorf("a certificate valid only from February to July 2015 gave the time %v", got)
		}
	}
}

func TestDateSources_AreHTTPS(t *testing.T) {
	for _, source := range DateSources {
		if !strings.HasPrefix(source, "https://") {
			t.Errorf("%s: a network with a login page could answer a plain-HTTP request with any time", source)
		}
	}
}
