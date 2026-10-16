package agent

// This file handles the extra pieces of WPA-Enterprise networks (eduroam,
// universities, companies): uploaded certificates and keys, and how they
// end up in wpa_supplicant's configuration.
//
// Certificates are stored as files in /persistent/rasbeacon/certs, named
// after the network's ID (see paths.Paths.CACertFile). The private key for
// EAP-TLS is readable by root only.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rasbeacon/internal/api"
	"rasbeacon/internal/certs"
	"rasbeacon/internal/config"
	"rasbeacon/internal/render"
)

// certUploads are the certificate files sent with one network, already
// checked and converted to PEM. Empty means "keep the saved file".
type certUploads struct {
	caCert     string
	clientCert string
	clientKey  string
}

// parseCertUploads checks the certificates and key uploaded for network
// number index. Errors name the field, e.g. "networks.0.enterprise.caCert".
func parseCertUploads(index int, e *api.EnterpriseSettings) (certUploads, error) {
	var uploads certUploads
	field := func(name string) string { return fmt.Sprintf("networks.%d.enterprise.%s", index, name) }
	if e.CACert != "" {
		pemText, _, err := certs.ParseCertificates(e.CACert)
		if err != nil {
			return uploads, &config.ValidationError{Field: field("caCert"), Message: err.Error()}
		}
		uploads.caCert = pemText
	}
	if e.ClientCert != "" {
		pemText, _, err := certs.ParseCertificates(e.ClientCert)
		if err != nil {
			return uploads, &config.ValidationError{Field: field("clientCert"), Message: err.Error()}
		}
		uploads.clientCert = pemText
	}
	if e.ClientKey != "" {
		pemText, err := certs.ParsePrivateKey(e.ClientKey)
		if err != nil {
			return uploads, &config.ValidationError{Field: field("clientKey"), Message: err.Error()}
		}
		uploads.clientKey = pemText
	}
	return uploads, nil
}

// credentialsFor says which password and certificates network id will have
// once uploads are saved (uploaded now, or saved earlier).
func (a *Agent) credentialsFor(id, password string, uploads certUploads) config.NetworkCredentials {
	return config.NetworkCredentials{
		Password:      password,
		HasCACert:     uploads.caCert != "" || fileExists(a.paths.CACertFile(id)),
		HasClientCert: uploads.clientCert != "" || fileExists(a.paths.ClientCertFile(id)),
		HasClientKey:  uploads.clientKey != "" || fileExists(a.paths.ClientKeyFile(id)),
	}
}

// saveCertUploads writes the uploaded files for network id.
func (a *Agent) saveCertUploads(id string, uploads certUploads) error {
	if err := os.MkdirAll(a.paths.CertDir(), 0o700); err != nil {
		return err
	}
	files := []struct {
		path    string
		content string
		mode    os.FileMode
	}{
		{a.paths.CACertFile(id), uploads.caCert, 0o644},
		{a.paths.ClientCertFile(id), uploads.clientCert, 0o644},
		{a.paths.ClientKeyFile(id), uploads.clientKey, 0o600}, // secret
	}
	for _, f := range files {
		if f.content == "" {
			continue // nothing uploaded: keep the saved file
		}
		if err := config.WriteFileAtomic(f.path, []byte(f.content), f.mode); err != nil {
			return err
		}
	}
	return nil
}

// removeUnusedCerts deletes the files of networks that are no longer saved.
func (a *Agent) removeUnusedCerts() {
	files, _ := filepath.Glob(filepath.Join(a.paths.CertDir(), "*.pem"))
	for _, path := range files {
		id, _, _ := strings.Cut(filepath.Base(path), "-")
		if !a.isSavedNetworkID(id) {
			_ = os.Remove(path)
		}
	}
}

// wpaEnterprise turns a saved network's Enterprise settings into what the
// wpa_supplicant renderer needs.
func (a *Agent) wpaEnterprise(n config.SavedNetwork) *render.WPAEnterprise {
	e := n.Enterprise
	if n.Security != config.UplinkEnterprise || e == nil {
		return nil
	}
	w := &render.WPAEnterprise{
		Method:            e.Method,
		Identity:          e.Identity,
		AnonymousIdentity: e.AnonymousIdentity,
	}
	if !e.AllowNoCertificateCheck {
		w.CACertFile = a.paths.CACertFile(n.ID)
		w.ServerDomain = e.ServerDomain()
	}
	if e.Method == config.EAPTLS {
		w.ClientCertFile = a.paths.ClientCertFile(n.ID)
		w.ClientKeyFile = a.paths.ClientKeyFile(n.ID)
	}
	return w
}

// enterpriseSettings describes a saved network's Enterprise settings for
// the settings page, with one-line summaries of its certificates.
func (a *Agent) enterpriseSettings(n config.SavedNetwork) *api.EnterpriseSettings {
	e := n.Enterprise
	if n.Security != config.UplinkEnterprise || e == nil {
		return nil
	}
	return &api.EnterpriseSettings{
		Method:                  e.Method,
		Identity:                e.Identity,
		AnonymousIdentity:       e.AnonymousIdentity,
		ServerName:              e.ServerName,
		AllowNoCertificateCheck: e.AllowNoCertificateCheck,
		CACertSummary:           a.certSummary(a.paths.CACertFile(n.ID)),
		ClientCertSummary:       a.certSummary(a.paths.ClientCertFile(n.ID)),
		HasClientKey:            fileExists(a.paths.ClientKeyFile(n.ID)),
	}
}

// certSummary describes a saved certificate file, or returns "" if there is
// none.
func (a *Agent) certSummary(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_, parsed, err := certs.ParseCertificates(string(data))
	if err != nil {
		return "unreadable certificate file"
	}
	return certs.Summary(parsed, a.now())
}

// enterpriseWarnings lists networks whose login server is not checked.
func (a *Agent) enterpriseWarnings() []string {
	var warnings []string
	for _, n := range a.cfg.Uplink.Networks {
		if n.Enterprise != nil && n.Enterprise.AllowNoCertificateCheck {
			warnings = append(warnings, fmt.Sprintf("The network %q does not check its login server's certificate. "+
				"Anyone nearby could pretend to be this network and collect your password. "+
				"Upload the CA certificate and server name on the Internet page.", n.SSID))
		}
	}
	return warnings
}

// MaskIdentity hides most of a username for logs and diagnostics:
// "NetID@rutgers.edu" becomes "N***@rutgers.edu".
func MaskIdentity(identity string) string {
	user, realm, hasRealm := strings.Cut(identity, "@")
	masked := "***"
	if first := []rune(user); len(first) > 0 {
		masked = string(first[0]) + "***"
	}
	if hasRealm {
		masked += "@" + realm
	}
	return masked
}

// fileExists reports whether a file (or folder) exists at path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
