// Package tlscert makes and checks the certificate for RasBeacon's HTTPS
// setting.
//
// By default RasBeacon makes its own ("self-signed") certificate. Browsers
// warn about those, because nobody vouched for it, but the connection is
// still encrypted. The settings page shows the certificate's fingerprint,
// so you can check that the one your browser shows is really RasBeacon's.
// You can also upload a certificate you made yourself, for example for a
// name in your own domain.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// Validity is how long a generated certificate lasts: 820 days. Apple's
// devices refuse server certificates valid for more than 825 days, so a
// longer one could make an iPhone or Mac refuse the settings page.
// RasBeacon makes a new one before it runs out (see RenewBefore).
const Validity = 820 * 24 * time.Hour

// RenewBefore is how long before it expires a generated certificate is
// replaced, so it never runs out on a trip.
const RenewBefore = 30 * 24 * time.Hour

// Generate makes a self-signed certificate valid for RasBeacon's possible
// hotspot addresses, the name rasbeacon.lan and any other hostnames. It
// returns the certificate and private key in PEM format.
func Generate(name string, hostnames []string, addresses []net.IP, now time.Time) (certPEM, keyPEM string, err error) {
	dnsNames := []string{"rasbeacon.lan"}
	for _, h := range hostnames {
		if h != dnsNames[0] {
			dnsNames = append(dnsNames, h)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		// A day early, in case the clock is a little behind.
		NotBefore:   now.Add(-24 * time.Hour),
		NotAfter:    now.Add(Validity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: addresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), nil
}

// Check makes sure an uploaded certificate (PEM, optionally followed by
// intermediate certificates) and private key (PEM, not password-protected)
// belong together and are usable.
func Check(certPEM, keyPEM string, now time.Time) error {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		switch {
		case !strings.Contains(certPEM, "BEGIN CERTIFICATE"):
			return errors.New("The certificate file must be in PEM format (text starting with -----BEGIN CERTIFICATE-----).")
		case strings.Contains(keyPEM, "ENCRYPTED"):
			return errors.New("The private key is password-protected. Remove the password first (for example with `openssl pkey -in key.pem -out plain.pem`).")
		default:
			return fmt.Errorf("The certificate and key don't work together: %v", err)
		}
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("This certificate expired on %s.", leaf.NotAfter.Format("2006-01-02"))
	}
	return nil
}

// Info describes a certificate for the settings page.
type Info struct {
	Subject string
	// Fingerprint is the SHA-256 of the certificate, as browsers show it
	// (pairs of hex digits separated by colons).
	Fingerprint string
	NotBefore   time.Time
	NotAfter    time.Time
}

// NeedsRenewal reports whether a generated certificate should be replaced
// at now: it runs out within RenewBefore, or it is valid for longer than
// Validity (RasBeacon 0.1 made them for ten years, which Apple's devices
// may refuse).
func (i Info) NeedsRenewal(now time.Time) bool {
	// The day of margin Generate gives NotBefore, and a day more to spare.
	tooLong := i.NotAfter.Sub(i.NotBefore) > Validity+2*24*time.Hour
	return tooLong || i.NotAfter.Sub(now) < RenewBefore
}

// Describe reads the first certificate in certPEM.
func Describe(certPEM string) (Info, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return Info{}, errors.New("no certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Info{}, err
	}
	sum := sha256.Sum256(cert.Raw)
	pairs := make([]string, len(sum))
	for i, b := range sum {
		pairs[i] = fmt.Sprintf("%02X", b)
	}
	return Info{Subject: cert.Subject.CommonName, Fingerprint: strings.Join(pairs, ":"), NotBefore: cert.NotBefore, NotAfter: cert.NotAfter}, nil
}
