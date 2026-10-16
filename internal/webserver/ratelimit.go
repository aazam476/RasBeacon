package webserver

// Login rate limiting: slows down anyone trying to guess the admin password.
//
// Rules:
//   - Per device (IP address): the first 5 wrong passwords are free. After
//     that, each attempt must wait twice as long as the previous one
//     (2 s, 4 s, 8 s, ... up to 5 minutes).
//   - Overall: after 20 wrong passwords within 5 minutes from any mix of
//     devices, all logins are paused for 5 minutes.
//
// A correct password resets the counter for that device.

import (
	"sync"
	"time"
)

const (
	freeAttempts        = 5
	maxBackoff          = 5 * time.Minute
	globalFailureLimit  = 20
	globalFailureWindow = 5 * time.Minute
	globalLockout       = 5 * time.Minute
)

type loginLimiter struct {
	mu  sync.Mutex
	now func() time.Time

	// Per IP address: how many failures in a row, and when the next attempt
	// is allowed.
	failures    map[string]int
	nextAllowed map[string]time.Time

	// Across all addresses: times of recent failures, and the end of a
	// global lockout (zero if none).
	recentFailures []time.Time
	lockedUntil    time.Time
}

// newLoginLimiter makes an empty limiter on the real clock.
func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		now:         time.Now,
		failures:    map[string]int{},
		nextAllowed: map[string]time.Time{},
	}
}

// allow reports whether ip may try a password now. If not, it also returns
// how long to wait.
func (l *loginLimiter) allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if now.Before(l.lockedUntil) {
		return false, l.lockedUntil.Sub(now)
	}
	if next, ok := l.nextAllowed[ip]; ok && now.Before(next) {
		return false, next.Sub(now)
	}
	return true, 0
}

// recordFailure notes a wrong password from ip.
func (l *loginLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	// Per-IP backoff.
	l.failures[ip]++
	if extra := l.failures[ip] - freeAttempts; extra > 0 {
		backoff := time.Second << extra // 2 s, 4 s, 8 s, ...
		if backoff > maxBackoff || backoff <= 0 {
			backoff = maxBackoff
		}
		l.nextAllowed[ip] = now.Add(backoff)
	}

	// Global limit: keep only failures from the last window, then add this one.
	cutoff := now.Add(-globalFailureWindow)
	kept := l.recentFailures[:0]
	for _, t := range l.recentFailures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.recentFailures = append(kept, now)
	if len(l.recentFailures) >= globalFailureLimit {
		l.lockedUntil = now.Add(globalLockout)
		l.recentFailures = nil
	}
}

// recordSuccess clears ip's failure count after a correct password.
func (l *loginLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
	delete(l.nextAllowed, ip)
}
