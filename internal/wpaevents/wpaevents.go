// Package wpaevents reads wpa_supplicant's events, such as "trying to join
// this network" or "the login server sent this certificate".
//
// wpa_supplicant has a control socket per interface (a Unix datagram
// socket in /run/wpa_supplicant). A program that sends it "ATTACH" gets
// every event from then on as a short text message, like
//
//	<3>CTRL-EVENT-EAP-PEER-CERT depth=0 subject='/CN=radius.example.com' hash=ab12…
//
// wpa_cli works the same way; reading the socket directly lets rbd react to
// events as they happen instead of polling.
package wpaevents

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"rasbeacon/internal/uplink"
)

// Listen attaches to the control socket of iface in ctrlDir and calls
// handle with each event (without its "<3>" priority). localDir is where
// its own socket is made (wpa_supplicant answers to it). It returns when
// keep reports false (checked every second) or the socket fails, for
// example because wpa_supplicant restarted; the caller simply calls it
// again.
func Listen(ctrlDir, iface, localDir string, keep func() bool, handle func(event string)) error {
	local := filepath.Join(localDir, fmt.Sprintf("wpaevents-%s-%d", iface, os.Getpid()))
	_ = os.Remove(local)
	conn, err := net.DialUnix("unixgram",
		&net.UnixAddr{Name: local, Net: "unixgram"},
		&net.UnixAddr{Name: filepath.Join(ctrlDir, iface), Net: "unixgram"})
	if err != nil {
		return err
	}
	defer os.Remove(local)
	defer conn.Close()

	if err := request(conn, "ATTACH", "OK"); err != nil {
		return err
	}
	defer func() { _, _ = conn.Write([]byte("DETACH")) }()

	buf := make([]byte, 4096)
	lastPing := time.Now()
	for keep() {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Timeout() {
				return err
			}
		} else if message := string(buf[:n]); strings.HasPrefix(message, "<") {
			_, event, _ := strings.Cut(message, ">")
			handle(strings.TrimSpace(event))
		}
		// A PING now and then notices a restarted wpa_supplicant (which
		// forgets who attached): no answer means start over.
		if time.Since(lastPing) > 30*time.Second {
			lastPing = time.Now()
			if _, err := conn.Write([]byte("PING")); err != nil {
				return err
			}
		}
	}
	return nil
}

// request sends a command and waits for its answer, skipping events.
func request(conn *net.UnixConn, command, want string) error {
	if _, err := conn.Write([]byte(command)); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		n, err := conn.Read(buf)
		if err != nil {
			return fmt.Errorf("%s: %w", command, err)
		}
		if reply := strings.TrimSpace(string(buf[:n])); !strings.HasPrefix(reply, "<") {
			if reply != want {
				return fmt.Errorf("%s: wpa_supplicant answered %q", command, reply)
			}
			return nil
		}
	}
}

// trying matches "Trying to associate with 00:11:22:33:44:55 (SSID='eduroam' freq=2412 MHz)".
var trying = regexp.MustCompile(`^Trying to associate with \S+ \(SSID='(.*)' freq=`)

// TryingSSID returns the network wpa_supplicant starts joining, if the
// event says so.
func TryingSSID(event string) (string, bool) {
	m := trying.FindStringSubmatch(event)
	if m == nil {
		return "", false
	}
	return uplink.DecodeSSID(m[1]), true
}

// peerCert matches the login server's own certificate (depth 0; higher
// depths are the CAs that signed it) and its SHA-256.
var peerCert = regexp.MustCompile(`^CTRL-EVENT-EAP-PEER-CERT depth=0 .*\bhash=([0-9a-f]{64})\b`)

// ServerCertHash returns the SHA-256 (hexadecimal) of the login server's
// certificate, if the event carries it.
func ServerCertHash(event string) (string, bool) {
	m := peerCert.FindStringSubmatch(event)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// CertificateRejected reports whether the event says the login server's
// certificate didn't match what was expected.
func CertificateRejected(event string) bool {
	return strings.HasPrefix(event, "CTRL-EVENT-EAP-TLS-CERT-ERROR")
}
