package netns

// DNS-over-HTTPS: devices' lookups go from dnsmasq to rbdoh (the real
// program) and on to a DoH server over HTTPS, never as plain DNS. rbdoh
// finds the DoH server's name with its bootstrap servers, the first of
// which doesn't answer.
//
//	[phone] ── dnsmasq ── rbdoh ──HTTPS──▶ DoH server  (upstream, 10.99.0.2:443)
//	                       └──plain DNS──▶ "doh.test" only (upstream, 10.99.0.2:53)

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoH_DevicesLookupsGoOverHTTPSNotPlainDNS(t *testing.T) {
	requireRoot(t)
	buildNetwork(t)
	dir := t.TempDir()
	certFile, keyFile := makeCertificate(t, dir, "doh.test")
	rbdoh := buildProgram(t, dir, "rasbeacon/cmd/rbdoh")

	// The internet's plain DNS server. It would answer example.test with a
	// different address (10.99.0.66) than the DoH server does, and logs
	// every question it gets.
	plainLog := filepath.Join(dir, "plain-dns.log")
	startBackground(t, upstreamNS, "dnsmasq", "--keep-in-foreground", "--no-resolv", "--no-hosts",
		"--listen-address=10.99.0.2", "--bind-interfaces",
		"--address=/doh.test/10.99.0.2", "--address=/example.test/10.99.0.66",
		"--log-queries", "--log-facility="+plainLog, "--pid-file=", "--user=root")
	// The DoH server (this test program, see TestMain and serveDoH).
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	startBackgroundWith(t, upstreamNS,
		[]string{"RB_DOH_SERVER=10.99.0.2:443", "RB_DOH_CERT=" + certFile, "RB_DOH_KEY=" + keyFile}, self)

	// RasBeacon: dnsmasq asks rbdoh, as rbd sets it up for DoH servers.
	startRouter(t, routerSettings{Normal: true, UpstreamDNS: "127.0.0.1#5053"})
	inNS(t, routerNS, "ip", "route", "add", "default", "via", "10.99.0.2")
	// 192.0.2.53 doesn't exist: the bootstrap falls back to the next one.
	startBackgroundWith(t, routerNS,
		[]string{"RBDOH_SERVERS=https://doh.test/dns-query", "RBDOH_BOOTSTRAP=192.0.2.53 10.99.0.2"},
		rbdoh, "-ca", certFile)
	givePhoneAddress(t)
	time.Sleep(500 * time.Millisecond)

	out, ok := tryInNS(phoneNS, 20*time.Second, "busybox", "nslookup", "-type=a", "example.test", "10.87.0.1")
	if !ok || !strings.Contains(out, "Address: 10.99.0.80") {
		t.Fatalf("the phone should get the DoH server's answer (10.99.0.80):\n%s", out)
	}
	// A second lookup uses the connection already open.
	if out, ok := tryInNS(phoneNS, 5*time.Second, "busybox", "nslookup", "-type=a", "www.example.test", "10.87.0.1"); !ok ||
		!strings.Contains(out, "Address: 10.99.0.80") {
		t.Errorf("second lookup:\n%s", out)
	}

	plain, _ := os.ReadFile(plainLog)
	if !strings.Contains(string(plain), "doh.test") {
		t.Errorf("rbdoh should look the DoH server's name up with the bootstrap server:\n%s", plain)
	}
	if strings.Contains(string(plain), "example.test") {
		t.Errorf("a device's lookup leaked as plain DNS:\n%s", plain)
	}
}

// serveDoH runs the RB_DOH_SERVER helper (see TestMain): a DoH server that
// answers every A question about example.test or its subdomains with
// 10.99.0.80, and others with no address.
func serveDoH(address, certFile, keyFile string) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(io.LimitReader(r.Body, 65535))
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/dns-message" || len(query) < 12 {
			http.Error(w, "not a DoH question", http.StatusBadRequest)
			return
		}
		name, end := questionName(query)
		if end == 0 {
			http.Error(w, "unreadable question", http.StatusBadRequest)
			return
		}
		qtype := binary.BigEndian.Uint16(query[end-4 : end-2])
		answer := append([]byte{}, query[:end]...)
		answer[2], answer[3] = 0x81, 0x80 // a response; recursion desired and available
		clear(answer[6:12])
		if qtype == 1 && (name == "example.test" || strings.HasSuffix(name, ".example.test")) {
			answer[7] = 1
			answer = append(answer, 0xC0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 10, 99, 0, 80)
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(answer)
	})
	server := &http.Server{Addr: address, Handler: handler}
	err := server.ListenAndServeTLS(certFile, keyFile)
	println("DoH server stopped:", err.Error())
	os.Exit(1)
}

// questionName reads the first question's name and where the question
// ends (0 if it can't be read).
func questionName(msg []byte) (string, int) {
	var labels []string
	at := 12
	for at < len(msg) {
		n := int(msg[at])
		if n == 0 {
			end := at + 1 + 4
			if end > len(msg) {
				return "", 0
			}
			return strings.ToLower(strings.Join(labels, ".")), end
		}
		if n&0xC0 != 0 || at+1+n > len(msg) {
			return "", 0
		}
		labels = append(labels, string(msg[at+1:at+1+n]))
		at += 1 + n
	}
	return "", 0
}

// makeCertificate writes a self-signed certificate for host and its key.
func makeCertificate(t *testing.T, dir, host string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	writeFile(t, certFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	writeFile(t, keyFile, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})))
	return certFile, keyFile
}

// buildProgram builds one of RasBeacon's programs into dir.
func buildProgram(t *testing.T, dir, pkg string) string {
	t.Helper()
	out := filepath.Join(dir, filepath.Base(pkg))
	if b, err := exec.Command("go", "build", "-o", out, pkg).CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", pkg, err, b)
	}
	return out
}

// startBackgroundWith is startBackground with extra environment variables.
func startBackgroundWith(t *testing.T, ns string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("ip", append([]string{"netns", "exec", ns}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %v: %v", args, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
}
