// Package certs reads the certificates and keys uploaded for WPA-Enterprise
// networks, and turns them into the PEM files wpa_supplicant reads.
//
// Institutions publish certificates in two formats, and the web UI accepts
// both:
//   - PEM: text starting with "-----BEGIN CERTIFICATE-----" (.pem, .crt)
//   - DER: the same data in binary (.der, .cer). The browser sends it
//     base64-encoded, because the API carries text.
package certs

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MaxUploadSize limits one uploaded certificate or key (after base64).
// Real CA bundles are a few kilobytes.
const MaxUploadSize = 32 * 1024

// ParseCertificates reads one or more certificates (PEM, or base64 DER) and
// returns them as PEM text plus the parsed certificates.
func ParseCertificates(upload string) (string, []*x509.Certificate, error) {
	if len(upload) > MaxUploadSize {
		return "", nil, errors.New("The file is too big to be a certificate.")
	}
	var der [][]byte
	if strings.Contains(upload, "-----BEGIN") {
		rest := []byte(upload)
		for {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type == "CERTIFICATE" {
				der = append(der, block.Bytes)
			}
		}
	} else {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(upload))
		if err != nil {
			return "", nil, errors.New("This is not a certificate file (expected .pem, .crt, .der or .cer).")
		}
		der = append(der, data)
	}
	if len(der) == 0 {
		return "", nil, errors.New("No certificate found in the file.")
	}

	var certificates []*x509.Certificate
	var out strings.Builder
	for _, block := range der {
		// A DER file may hold several certificates back to back.
		parsed, err := x509.ParseCertificates(block)
		if err != nil {
			return "", nil, errors.New("The certificate could not be read: " + err.Error())
		}
		for _, c := range parsed {
			certificates = append(certificates, c)
			_ = pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
		}
	}
	if len(certificates) == 0 {
		return "", nil, errors.New("No certificate found in the file.")
	}
	return out.String(), certificates, nil
}

// ParsePrivateKey reads a private key (PEM, or base64 DER) for EAP-TLS and
// returns it as PEM text. Password-protected keys are accepted as they are;
// wpa_supplicant unlocks them with the passphrase.
func ParsePrivateKey(upload string) (string, error) {
	if len(upload) > MaxUploadSize {
		return "", errors.New("The file is too big to be a private key.")
	}
	if strings.Contains(upload, "-----BEGIN") {
		block, _ := pem.Decode([]byte(upload))
		if block == nil || !strings.Contains(block.Type, "PRIVATE KEY") {
			return "", errors.New("No private key found in the file.")
		}
		encrypted := block.Type == "ENCRYPTED PRIVATE KEY" || block.Headers["Proc-Type"] != ""
		if !encrypted && !isPrivateKey(block.Bytes) {
			return "", errors.New("The private key could not be read.")
		}
		return string(pem.EncodeToMemory(block)), nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(upload))
	if err != nil || !isPrivateKey(data) {
		return "", errors.New("This is not a private key file (expected .pem, .key or .der).")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(data)})), nil
}

// isPrivateKey reports whether der is an unencrypted private key in one of
// the common formats (PKCS #8, PKCS #1 RSA, or SEC 1 EC).
func isPrivateKey(der []byte) bool {
	if _, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return true
	}
	if _, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return true
	}
	_, err := x509.ParseECPrivateKey(der)
	return err == nil
}

// mustPKCS8 converts a valid DER private key to PKCS #8, the format the
// "PRIVATE KEY" PEM type expects.
func mustPKCS8(der []byte) []byte {
	if _, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return der
	}
	var key any
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		key = k
	} else if k, err := x509.ParseECPrivateKey(der); err == nil {
		key = k
	}
	out, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return der
	}
	return out
}

// Summary describes certificates in one line for the settings page, e.g.
// "USERTrust RSA Certification Authority (valid until 2038-01-18)".
func Summary(certificates []*x509.Certificate, now time.Time) string {
	var parts []string
	for _, c := range certificates {
		name := c.Subject.CommonName
		if name == "" {
			name = c.Subject.String()
		}
		state := "valid until"
		if now.After(c.NotAfter) {
			state = "EXPIRED on"
		}
		parts = append(parts, fmt.Sprintf("%s (%s %s)", name, state, c.NotAfter.Format("2006-01-02")))
	}
	return strings.Join(parts, "; ")
}
