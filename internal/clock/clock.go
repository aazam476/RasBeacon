// Package clock helps RasBeacon know the time. A Raspberry Pi has no
// battery-backed clock, so after power-off it starts from an old time.
// That matters: WireGuard servers reject handshakes that look older than
// the last one, and certificate checks (eduroam) fail if the clock is far
// behind.
//
// RasBeacon uses two sources:
//
//  1. A "time floor": the time saved on the data partition every 15 minutes
//     and at shutdown. At boot the clock is moved forward to it, so time
//     never goes backwards across a reboot (see the agent's clock.go).
//  2. Once there is an uplink, the "Date" header of HTTPS requests to
//     well-known servers. A time is accepted only if at least two servers
//     agree (AgreedTime).
//
// Why not NTP, as computers usually do: an NTP answer can't be checked. A
// network names its own time server when a device joins it, and the
// networks RasBeacon is made for (a hotel's, a café's) aren't trusted. A
// time years ahead from one of them would be saved as the time floor, so
// every later boot would start from it; and a WireGuard server that once
// saw a handshake from then refuses those with the right time. The VPN
// and every secure connection would break long after leaving that network.
// Networks with a login page also block NTP, and answer every plain web
// request themselves: over plain HTTP they could make all the servers
// "agree" on any time.
//
// Over HTTPS, only the real servers can answer. Their certificates are
// checked as usual, except that the clock isn't used to check their dates
// (it is the clock that is being set): a certificate must be valid at the
// time its server says it is instead (see Client and FetchDate).
package clock

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"sort"
	"time"
)

// DateSources are HTTPS addresses of three different companies that answer
// quickly and include a "Date" header.
var DateSources = []string{
	"https://www.google.com/generate_204",
	"https://www.apple.com/library/test/success.html",
	"https://www.cloudflare.com/cdn-cgi/trace",
}

// Client returns the HTTP client for FetchDate. It checks each server's
// certificate the usual way, signed by an authority in roots (nil: the
// system's) for the server's name, but as of the day the certificate
// became valid instead of today: a clock that is wrong is what this asks
// for, and a certificate would look expired, or not valid yet, to it.
// FetchDate then checks the certificate's dates against the time the
// server gives.
func Client(roots *x509.CertPool) *http.Client {
	verify := func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 || state.ServerName == "" {
			return errors.New("no certificate or server name")
		}
		leaf := state.PeerCertificates[0]
		intermediates := x509.NewCertPool()
		for _, cert := range state.PeerCertificates[1:] {
			intermediates.AddCert(cert)
		}
		_, err := leaf.Verify(x509.VerifyOptions{
			DNSName:       state.ServerName,
			Roots:         roots,
			Intermediates: intermediates,
			CurrentTime:   leaf.NotBefore,
		})
		return err
	}
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				// Go's own check includes the dates, so it is switched off
				// and verify does the same check without them.
				InsecureSkipVerify: true,
				VerifyConnection:   verify,
			},
		},
	}
}

// MaxDisagreement is how far apart two servers' times may be and still
// count as agreeing.
const MaxDisagreement = 5 * time.Second

// FetchDate asks url for the time from its "Date" header. The answer must
// come over HTTPS, and the time must be one at which the server's
// certificate is valid: whoever holds an old certificate that got out,
// with its key, can then only give a time from when it was valid, never
// one after it expired.
func FetchDate(ctx context.Context, client *http.Client, url string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	resp.Body.Close()
	date, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return time.Time{}, err
	}
	// (Also after a redirect: the answer that counts is the last one.)
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return time.Time{}, errors.New("the time didn't come over HTTPS")
	}
	if leaf := resp.TLS.PeerCertificates[0]; date.Before(leaf.NotBefore) || date.After(leaf.NotAfter) {
		return time.Time{}, errors.New("the server's certificate isn't valid at the time the server gives")
	}
	return date, nil
}

// AgreedTime picks a time that at least two samples agree on (within
// MaxDisagreement). It returns false if there is no such time, for example
// when only one server answered.
//
// The time may be earlier than the clock, or than the saved time floor:
// the servers are who they say they are (see FetchDate), and a clock that
// is ahead, as one set by a network's time server in an older version
// could be, has to be put right too.
func AgreedTime(samples []time.Time) (time.Time, bool) {
	var valid []time.Time
	for _, s := range samples {
		if !s.IsZero() {
			valid = append(valid, s)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Before(valid[j]) })
	// After sorting, agreeing samples are next to each other.
	for i := 0; i+1 < len(valid); i++ {
		if valid[i+1].Sub(valid[i]) <= MaxDisagreement {
			return valid[i+1], true
		}
	}
	return time.Time{}, false
}
