package webserver

// Login rate limiting: slows down anyone trying to guess the admin password.
//
// Rules, per device (IP address):
//   - The first 5 wrong passwords are free.
//   - After that, each attempt must wait twice as long as the previous one:
//     2 s, 4 s, 8 s, ... up to an hour.
//   - A correct password resets the count; so does a day without a wrong
//     one.
//
// There is deliberately no limit across devices: one would let any device
// on the hotspot keep the owner out by failing on purpose. Guessing from
// many addresses at once is still slow: each check takes about a third of
// a second on purpose, and checks run one at a time (see passwords.go).

import (
	"sync"
	"time"
)

// The rules above, as numbers.
const (
	freeAttempts = 5              // wrong passwords per device before it must wait
	maxBackoff   = time.Hour      // the longest wait for one device
	forgetAfter  = 24 * time.Hour // a device's count is forgotten this long after its last wrong password
	// maxTracked is how many devices' counts are kept before the forgotten
	// ones are cleared out, so a device changing its address all the time
	// can't make the list grow without end.
	maxTracked = 1024
)

// deviceFailures is one device's wrong passwords: how many in a row, when
// the last one was, and when it may try again.
type deviceFailures struct {
	count       int
	last, until time.Time
}

// loginLimiter slows down password guessing: it counts failed logins per
// address and refuses logins from that address for a while after too
// many.
type loginLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	devices map[string]deviceFailures
}

// newLoginLimiter makes an empty limiter on the real clock.
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{now: time.Now, devices: map[string]deviceFailures{}}
}

// allow reports whether ip may try a password now. If not, it also returns
// how long to wait.
func (l *loginLimiter) allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if d, ok := l.devices[ip]; ok && now.Before(d.until) {
		return false, d.until.Sub(now)
	}
	return true, 0
}

// recordFailure notes a wrong password from ip.
func (l *loginLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	d := l.devices[ip]
	if now.Sub(d.last) >= forgetAfter {
		d = deviceFailures{}
	}
	d.count++
	d.last = now
	if extra := d.count - freeAttempts; extra > 0 {
		backoff := maxBackoff
		if extra < 32 {
			backoff = min(time.Second<<extra, maxBackoff) // 2 s, 4 s, 8 s, ...
		}
		d.until = now.Add(backoff)
	}
	l.devices[ip] = d
	if len(l.devices) > maxTracked {
		for other, f := range l.devices {
			if now.Sub(f.last) >= forgetAfter {
				delete(l.devices, other)
			}
		}
	}
}

// recordSuccess clears ip's failure count after a correct password.
func (l *loginLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.devices, ip)
}
