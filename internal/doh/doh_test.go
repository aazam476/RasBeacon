package doh

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// question builds a DNS question for name (type A), with an EDNS record
// offering size bytes over UDP when size > 0.
func question(id uint16, name string, size uint16) []byte {
	msg := binary.BigEndian.AppendUint16(nil, id)
	msg = append(msg, 0x01, 0x00) // RD
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = append(msg, 0, 0, 0, 0)
	if size > 0 {
		msg = binary.BigEndian.AppendUint16(msg, 1)
	} else {
		msg = append(msg, 0, 0)
	}
	for _, label := range strings.Split(name, ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0, 0, 1, 0, 1) // root, type A, class IN
	if size > 0 {
		msg = append(msg, 0, 0, 41)
		msg = binary.BigEndian.AppendUint16(msg, size)
		msg = append(msg, 0, 0, 0, 0, 0, 0)
	}
	return msg
}

// answerTo answers a question with extra bytes standing in for records.
func answerTo(q []byte, extra int) []byte {
	a := bytes.Clone(q[:questionEnd(q)])
	a[2] |= 0x80
	a[3] = 0x80
	return append(a, make([]byte, extra)...)
}

// dohServer is a DoH server that answers every question, counting them.
func dohServer(t *testing.T, extra int, asked *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		q, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != contentType || len(q) < headerLen {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		if q[0] != 0 || q[1] != 0 {
			http.Error(w, "the ID must be 0", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(answerTo(q, extra))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExchange_AsksOverHTTPSAndKeepsTheID(t *testing.T) {
	var asked atomic.Int32
	srv := dohServer(t, 16, &asked)
	f, err := New(Options{Servers: []string{srv.URL + "/dns-query"}, Transport: srv.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	answer := f.Exchange(context.Background(), question(0xBEEF, "example.org", 1232))
	if answer[0] != 0xBE || answer[1] != 0xEF || answer[2]&0x80 == 0 || answer[3]&0x0F != 0 {
		t.Fatalf("answer = %x", answer)
	}
	if len(answer) != questionEnd(question(0, "example.org", 0))+16 {
		t.Errorf("answer length %d", len(answer))
	}
}

func TestExchange_MovesOnAndSkipsAFailedServerForAWhile(t *testing.T) {
	var deadAsked, goodAsked atomic.Int32
	dead := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadAsked.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer dead.Close()
	good := dohServer(t, 0, &goodAsked)
	// Both test servers trust the same test certificate.
	f, err := New(Options{Servers: []string{dead.URL, good.URL}, Transport: good.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if a := f.Exchange(context.Background(), question(1, "example.org", 0)); a[3]&0x0F != 0 {
			t.Fatalf("rcode %d", a[3]&0x0F)
		}
	}
	if deadAsked.Load() != 1 || goodAsked.Load() != 3 {
		t.Errorf("dead asked %d times, good %d: a server that just failed should be skipped", deadAsked.Load(), goodAsked.Load())
	}
	// After the pause, the first server is asked first again.
	later := time.Now().Add(failurePause + time.Second)
	f.now = func() time.Time { return later }
	f.Exchange(context.Background(), question(1, "example.org", 0))
	if deadAsked.Load() != 2 {
		t.Errorf("dead asked %d times: the list should start from the first server again", deadAsked.Load())
	}
}

func TestExchange_ServerFailureWhenNoneAnswers(t *testing.T) {
	f, err := New(Options{Servers: []string{"https://127.0.0.1:1/dns-query"}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	q := question(7, "example.org", 1232)
	a := f.Exchange(context.Background(), q)
	if a[0] != 0 || a[1] != 7 || a[3]&0x0F != 2 || a[2]&0x01 == 0 {
		t.Errorf("want SERVFAIL with the ID and RD kept, got %x", a)
	}
	if !bytes.Equal(a[headerLen:], q[headerLen:questionEnd(q)]) || binary.BigEndian.Uint16(a[10:12]) != 0 {
		t.Errorf("the answer repeats the question only: %x", a)
	}
}

func TestBootstrap_FallsBackInTurnThenToTheLastKnownAddresses(t *testing.T) {
	var asked atomic.Int32
	srv := dohServer(t, 0, &asked)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	var tried []string
	failing := map[string]bool{"9.9.9.9": true}
	lookup := func(_ context.Context, server, host string) ([]netip.Addr, error) {
		tried = append(tried, server+" "+host)
		if failing[server] {
			return nil, errors.New("timeout")
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	// The test certificate is for example.com.
	f, err := New(Options{
		Servers:   []string{"https://example.com:" + port + "/dns-query"},
		Bootstrap: []string{"9.9.9.9", "127.0.0.53"},
		Lookup:    lookup,
	})
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	f.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}

	if a := f.Exchange(context.Background(), question(1, "example.org", 0)); a[3]&0x0F != 0 {
		t.Fatalf("rcode %d", a[3]&0x0F)
	}
	if strings.Join(tried, ", ") != "9.9.9.9 example.com, 127.0.0.53 example.com" {
		t.Errorf("tried %v: the owner's server first, then the network's", tried)
	}

	// Every bootstrap server fails, after the answer went stale: the last
	// known addresses still work.
	failing["127.0.0.53"] = true
	f.mu.Lock()
	known := f.known["example.com"]
	known.at = time.Now().Add(-time.Hour)
	f.known["example.com"] = known
	f.mu.Unlock()
	f.client.CloseIdleConnections()
	if a := f.Exchange(context.Background(), question(2, "example.org", 0)); a[3]&0x0F != 0 {
		t.Fatalf("rcode %d with the last known addresses", a[3]&0x0F)
	}
	if asked.Load() != 2 {
		t.Errorf("asked %d times", asked.Load())
	}
}

func TestServeUDP_TooBigAnswersAreTruncatedSoTheAskerUsesTCP(t *testing.T) {
	var asked atomic.Int32
	srv := dohServer(t, 2000, &asked)
	f, err := New(Options{Servers: []string{srv.URL}, Transport: srv.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() { _ = f.ServeUDP(udp) }()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() { _ = f.ServeTCP(tcp) }()

	conn, err := net.Dial("udp", udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(question(3, "example.org", 1232)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf[2]&0x02 == 0 || n > 512 {
		t.Errorf("want a truncated answer, got %d bytes, flags %x", n, buf[2])
	}

	// Over TCP the whole answer arrives.
	c, err := net.Dial("tcp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	q := question(4, "example.org", 1232)
	if _, err := c.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(q))), q...)); err != nil {
		t.Fatal(err)
	}
	var size uint16
	if err := binary.Read(c, binary.BigEndian, &size); err != nil {
		t.Fatal(err)
	}
	answer := make([]byte, size)
	if _, err := io.ReadFull(c, answer); err != nil {
		t.Fatal(err)
	}
	if answer[1] != 4 || answer[2]&0x02 != 0 || len(answer) < 2000 {
		t.Errorf("TCP answer: %d bytes, flags %x", len(answer), answer[2])
	}
}

func TestUDPLimit(t *testing.T) {
	if got := udpLimit(question(1, "example.org", 0)); got != 512 {
		t.Errorf("without EDNS: %d", got)
	}
	if got := udpLimit(question(1, "example.org", 1232)); got != 1232 {
		t.Errorf("with EDNS: %d", got)
	}
	if got := udpLimit([]byte{1, 2}); got != 512 {
		t.Errorf("garbage: %d", got)
	}
}
