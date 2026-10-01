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
//     "Date" header of plain web requests to well-known servers. A time is
//     accepted only if at least two servers agree (AgreedTime).
package clock

import (
	"context"
	"net/http"
	"sort"
	"time"
)

// DateSources are plain-HTTP addresses that answer quickly and include a
// "Date" header. They are the same "is there internet?" checks phones use.
var DateSources = []string{
	"http://captive.apple.com/",
	"http://connectivitycheck.gstatic.com/generate_204",
	"http://detectportal.firefox.com/success.txt",
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
