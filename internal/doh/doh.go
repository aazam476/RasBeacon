// Package doh is RasBeacon's DNS-over-HTTPS forwarder (run as cmd/rbdoh).
// dnsmasq passes devices' questions to it on the loopback address as plain
// DNS, and it asks the owner's DoH servers over HTTPS (RFC 8484), so the
// network RasBeacon is on can't read or change the answers.
//
// The DoH servers are named by host name (dns.google), and that name has to
// be looked up first, without DoH. That lookup ("bootstrap") asks, in turn,
// the owner's plain DNS servers and then the network's own (RasBeacon's
// resolver, 127.0.0.53). The answer is kept, so if every bootstrap server
// fails later, the last known addresses are used.
package doh

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"
)

const (
	// contentType is the media type of DNS messages over HTTPS.
	contentType = "application/dns-message"
	// maxMessage is the largest DNS message (it travels with a 16-bit
	// length over TCP).
	maxMessage = 65535
	// headerLen is the fixed DNS header: ID, flags and four counts.
	headerLen = 12
	// freshFor is how long a bootstrap answer is used before it is looked
	// up again (the stale one remains the fallback).
	freshFor = 10 * time.Minute
)

// Options configure a Forwarder.
type Options struct {
	// Servers are the DoH servers' URLs, asked in turn (the last one that
	// answered first).
	Servers []string
	// Bootstrap are plain DNS servers (IP addresses, port 53) that look up
	// the DoH servers' host names, asked in turn.
	Bootstrap []string
	// Timeout limits each question to one DoH server. Default 4 s.
	Timeout time.Duration
	// Lookup replaces the bootstrap lookup (tests).
	Lookup func(ctx context.Context, server, host string) ([]netip.Addr, error)
	// Transport replaces the HTTPS transport (tests). Its dialing is not
	// replaced, so the bootstrap is bypassed.
	Transport http.RoundTripper
	// RootCAs replaces the system's certificate authorities (tests).
	RootCAs *x509.CertPool
}

// Forwarder answers plain DNS questions by asking DoH servers.
type Forwarder struct {
	servers   []*url.URL
	bootstrap []string
	timeout   time.Duration
	lookup    func(ctx context.Context, server, host string) ([]netip.Addr, error)
	client    *http.Client

	mu        sync.Mutex
	preferred int                   // the server that answered last
	known     map[string]knownAddrs // bootstrap answers by host name
}

type knownAddrs struct {
	addrs []netip.Addr
	at    time.Time
}

// New checks the options and makes a Forwarder.
func New(o Options) (*Forwarder, error) {
	f := &Forwarder{
		bootstrap: o.Bootstrap,
		timeout:   o.Timeout,
		lookup:    o.Lookup,
		known:     map[string]knownAddrs{},
	}
	if len(o.Servers) == 0 {
		return nil, errors.New("no DoH servers")
	}
	for _, s := range o.Servers {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%q is not an https:// address", s)
		}
		f.servers = append(f.servers, u)
	}
	for _, b := range o.Bootstrap {
		if _, err := netip.ParseAddr(b); err != nil {
			return nil, fmt.Errorf("bootstrap server %q is not an IP address", b)
		}
	}
	if f.timeout <= 0 {
		f.timeout = 4 * time.Second
	}
	if f.lookup == nil {
		f.lookup = lookupWith
	}
	transport := o.Transport
	if transport == nil {
		transport = &http.Transport{
			DialContext:           f.dial,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: f.timeout,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   4,
		}
		if o.RootCAs != nil {
			transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: o.RootCAs}
		}
	}
	f.client = &http.Client{Transport: transport}
	return f, nil
}

// Exchange answers one DNS question (a whole message), asking the DoH
// servers in turn. When none answers, the answer is SERVFAIL, so the
// device's resolver moves on instead of waiting.
func (f *Forwarder) Exchange(ctx context.Context, query []byte) []byte {
	if len(query) < headerLen {
		return nil
	}
	// The ID is 0 over HTTPS, so answers can be cached (RFC 8484 4.1).
	q := bytes.Clone(query)
	q[0], q[1] = 0, 0

	f.mu.Lock()
	first := f.preferred
	f.mu.Unlock()
	var lastErr error
	for i := range f.servers {
		n := (first + i) % len(f.servers)
		answer, err := f.ask(ctx, f.servers[n], q)
		if err != nil {
			lastErr = err
			continue
		}
		f.mu.Lock()
		f.preferred = n
		f.mu.Unlock()
		answer[0], answer[1] = query[0], query[1]
		return answer
	}
	slog.Warn("no DoH server answered", "error", lastErr)
	return serverFailure(query)
}

func (f *Forwarder) ask(ctx context.Context, server *url.URL, q []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.String(), bytes.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", contentType)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", server.Host, resp.Status)
	}
	if got := resp.Header.Get("Content-Type"); got != contentType {
		return nil, fmt.Errorf("%s answered with %q, not DNS", server.Host, got)
	}
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxMessage+1))
	if err != nil {
		return nil, err
	}
	if len(answer) < headerLen || len(answer) > maxMessage {
		return nil, fmt.Errorf("%s answered with %d bytes", server.Host, len(answer))
	}
	return answer, nil
}

// dial connects to a DoH server, looking up its host name first.
func (f *Forwarder) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var addrs []netip.Addr
	if addr, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{addr}
	} else if addrs, err = f.resolve(ctx, host); err != nil {
		return nil, err
	}
	var d net.Dialer
	var lastErr error
	for _, addr := range addrs {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// resolve looks up a DoH server's host name with the bootstrap servers,
// falling back to the last addresses found.
func (f *Forwarder) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	known, ok := f.known[host]
	f.mu.Unlock()
	if ok && time.Since(known.at) < freshFor {
		return known.addrs, nil
	}
	var lastErr error = errors.New("no bootstrap DNS servers")
	for _, server := range f.bootstrap {
		addrs, err := f.lookup(ctx, server, host)
		if err == nil && len(addrs) > 0 {
			f.mu.Lock()
			f.known[host] = knownAddrs{addrs: addrs, at: time.Now()}
			f.mu.Unlock()
			return addrs, nil
		}
		if err == nil {
			err = fmt.Errorf("%s has no addresses", host)
		}
		lastErr = err
	}
	if ok {
		slog.Warn("looking up the DoH server failed; using its last known addresses", "host", host, "error", lastErr)
		return known.addrs, nil
	}
	return nil, fmt.Errorf("looking up %s: %w", host, lastErr)
}

// lookupWith asks one plain DNS server for a host name's addresses.
func lookupWith(ctx context.Context, server, host string) ([]netip.Addr, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		},
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	for i := range addrs {
		addrs[i] = addrs[i].Unmap()
	}
	return addrs, err
}

// --- Serving dnsmasq ---

// ServeUDP answers questions arriving on conn until it is closed.
func (f *Forwarder) ServeUDP(conn net.PacketConn) error {
	busy := make(chan struct{}, 64) // at most 64 questions at once
	for {
		buf := make([]byte, maxMessage)
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		busy <- struct{}{}
		go func(query []byte) {
			defer func() { <-busy }()
			answer := f.Exchange(context.Background(), query)
			if answer == nil {
				return
			}
			if limit := udpLimit(query); len(answer) > limit {
				answer = truncated(query)
			}
			_, _ = conn.WriteTo(answer, from)
		}(buf[:n])
	}
}

// ServeTCP answers questions on connections to l until it is closed.
func (f *Forwarder) ServeTCP(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go f.serveConn(conn)
	}
}

func (f *Forwarder) serveConn(conn net.Conn) {
	defer conn.Close()
	for {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
		var size uint16
		if err := binary.Read(conn, binary.BigEndian, &size); err != nil {
			return
		}
		query := make([]byte, size)
		if _, err := io.ReadFull(conn, query); err != nil {
			return
		}
		answer := f.Exchange(context.Background(), query)
		if answer == nil {
			return
		}
		out := binary.BigEndian.AppendUint16(nil, uint16(len(answer)))
		if _, err := conn.Write(append(out, answer...)); err != nil {
			return
		}
	}
}

// --- DNS message details ---

// questionEnd returns where the question section ends (just past the
// header when it can't be read).
func questionEnd(msg []byte) int {
	if len(msg) < headerLen {
		return len(msg)
	}
	at := headerLen
	for range binary.BigEndian.Uint16(msg[4:6]) {
		for {
			if at >= len(msg) {
				return headerLen
			}
			label := int(msg[at])
			if label == 0 {
				at++
				break
			}
			if label&0xC0 != 0 { // compressed: not in questions we send on
				return headerLen
			}
			at += 1 + label
		}
		at += 4 // type and class
		if at > len(msg) {
			return headerLen
		}
	}
	return at
}

// udpLimit is the largest answer the asker takes over UDP: its EDNS buffer
// size (RFC 6891), or 512 without EDNS.
func udpLimit(query []byte) int {
	at := questionEnd(query)
	if at == headerLen || len(query) < headerLen {
		return 512
	}
	// dnsmasq's questions have no answer or authority records; the EDNS
	// record (type 41) is the additional one, with the root name.
	if binary.BigEndian.Uint16(query[6:8]) != 0 || binary.BigEndian.Uint16(query[8:10]) != 0 {
		return 512
	}
	if binary.BigEndian.Uint16(query[10:12]) >= 1 && at+5 <= len(query) && query[at] == 0 &&
		binary.BigEndian.Uint16(query[at+1:at+3]) == 41 {
		if size := int(binary.BigEndian.Uint16(query[at+3 : at+5])); size > 512 {
			return size
		}
	}
	return 512
}

// reply starts an answer to query: its ID, question, and flags with QR,
// RA and the given rcode set. The counts other than questions are 0.
func reply(query []byte, rcode byte, truncate bool) []byte {
	end := questionEnd(query)
	msg := bytes.Clone(query[:end])
	msg[2] = 0x80 | query[2]&0x79 // QR, the asker's opcode and RD
	if truncate {
		msg[2] |= 0x02
	}
	msg[3] = 0x80 | rcode // RA
	if end == headerLen {
		msg[4], msg[5] = 0, 0
	}
	clear(msg[6:12])
	return msg
}

// serverFailure is the SERVFAIL answer to query.
func serverFailure(query []byte) []byte { return reply(query, 2, false) }

// truncated tells the asker the answer is too big for UDP, so it asks
// again over TCP.
func truncated(query []byte) []byte { return reply(query, 0, true) }
