// Package clock helps RasBeacon know the time. A Raspberry Pi has no
// battery-backed clock, so after power-off it starts from an old time.
// That matters: WireGuard servers reject handshakes that look older than
// the last one, and certificate checks (eduroam) fail if the clock is far
// behind.
//
// RasBeacon uses three sources:
//
//  1. A "time floor": the time saved on the data partition every 15 minutes
//     and at shutdown. At boot the clock is moved forward to it, so time
//     never goes backwards across a reboot (see the agent's clock.go).
//  2. NTP, through systemd-timesyncd, once there is an uplink.
//  3. If NTP doesn't work within a while (some networks block it), the
//     "Date" header of HTTPS requests to well-known servers. A time is
//     accepted only if at least two servers agree (AgreedTime).
//
// Why HTTPS: networks with a login page block NTP and answer every plain
// web request themselves, so over plain HTTP they could make all three
// servers "agree" on a time years ahead. The time floor never goes back,
// so every later boot would jump ahead again, and the VPN would break long
// after leaving that network. Over HTTPS, only the real servers can answer.
// Their certificates are checked as usual, except for their dates (see
// Client): the clock that would check the dates is the one being set.
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

// FetchDate asks url for the time from its "Date" header.
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
	return http.ParseTime(resp.Header.Get("Date"))
}

// AgreedTime picks a time that at least two samples agree on (within
// MaxDisagreement), and that is not before floor. It returns false if there
// is no such time, for example when only one server answered or a captive
// portal returned a wrong date.
func AgreedTime(samples []time.Time, floor time.Time) (time.Time, bool) {
	var valid []time.Time
	for _, s := range samples {
		if !s.IsZero() && !s.Before(floor) {
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
