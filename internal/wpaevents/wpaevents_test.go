package wpaevents

import (
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParsingEvents(t *testing.T) {
	if ssid, ok := TryingSSID("Trying to associate with 00:11:22:33:44:55 (SSID='Campus Wi\\xe2\\x80\\x91Fi' freq=2412 MHz)"); !ok || ssid != "Campus Wi‑Fi" {
		t.Errorf("TryingSSID = %q, %v", ssid, ok)
	}
	hash := strings.Repeat("0f", 32)
	if got, ok := ServerCertHash("CTRL-EVENT-EAP-PEER-CERT depth=0 subject='/CN=radius.example.com' hash=" + hash); !ok || got != hash {
		t.Errorf("ServerCertHash = %q, %v", got, ok)
	}
	// A CA's certificate (depth 1) is not the server's.
	if _, ok := ServerCertHash("CTRL-EVENT-EAP-PEER-CERT depth=1 subject='/CN=Example CA' hash=" + hash); ok {
		t.Error("only the server's own certificate (depth 0) counts")
	}
	if !CertificateRejected("CTRL-EVENT-EAP-TLS-CERT-ERROR reason=8 depth=0 subject='/CN=x' err='Server certificate mismatch'") {
		t.Error("a certificate error should be reported")
	}
}

func TestListen_AttachesAndPassesEventsOn(t *testing.T) {
	dir := t.TempDir()
	// A stand-in for wpa_supplicant's control socket.
	server, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "wlan1"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		buf := make([]byte, 256)
		for {
			n, from, err := server.ReadFromUnix(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) == "ATTACH" {
				_, _ = server.WriteToUnix([]byte("OK\n"), from)
				_, _ = server.WriteToUnix([]byte("<3>CTRL-EVENT-SCAN-STARTED "), from)
			}
		}
	}()

	var mu sync.Mutex
	var events []string
	deadline := time.Now().Add(3 * time.Second)
	keep := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) == 0 && time.Now().Before(deadline)
	}
	err = Listen(dir, "wlan1", dir, keep, func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	if err != nil || len(events) != 1 || events[0] != "CTRL-EVENT-SCAN-STARTED" {
		t.Errorf("events %q, err %v", events, err)
	}
}
