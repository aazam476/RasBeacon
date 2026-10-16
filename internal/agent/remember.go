package agent

// "Stay logged in" on the login page.
//
// rbweb keeps login sessions in memory only, so they end when RasBeacon
// restarts. A browser that chose "Stay logged in" also gets a long random
// token in a second cookie. rbd keeps the tokens' SHA-256 hashes (never
// the tokens) in remembered-logins.json on the data partition, so the
// browser is logged in again without a password after a restart, for
// rememberTime from the login. Logging out forgets the token; a factory
// reset forgets them all.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"time"

	"rasbeacon/internal/config"
)

// rememberTime is how long "Stay logged in" lasts.
const rememberTime = 30 * 24 * time.Hour

// maxRememberedLogins limits the list; the oldest login is forgotten first.
const maxRememberedLogins = 20

// rememberedLogin is one browser that chose "Stay logged in".
type rememberedLogin struct {
	// Hash is the SHA-256 of the token, in hex.
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

// readRememberedLogins returns the logins that haven't expired.
func (a *Agent) readRememberedLogins() []rememberedLogin {
	var logins []rememberedLogin
	if err := config.ReadJSON(a.paths.RememberedLoginsFile(), &logins); err != nil && !errors.Is(err, config.ErrNotFound) {
		return nil // unreadable: everyone simply logs in again
	}
	now := a.now()
	return slices.DeleteFunc(logins, func(l rememberedLogin) bool { return !now.Before(l.Expires) })
}

// hashToken is the SHA-256 of a remembered-login token, as kept on disk
// (the token itself never is).
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RememberLogin makes a new token for a browser that chose "Stay logged in"
// (the caller has just checked the password).
func (a *Agent) RememberLogin() (token string, expires time.Time, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	expires = a.now().Add(rememberTime)
	logins := append(a.readRememberedLogins(), rememberedLogin{Hash: hashToken(token), Expires: expires})
	if len(logins) > maxRememberedLogins {
		logins = logins[len(logins)-maxRememberedLogins:]
	}
	if err := config.WriteJSON(a.paths.RememberedLoginsFile(), logins, 0o600); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// CheckRememberedLogin reports whether a token belongs to a remembered
// login that hasn't expired.
func (a *Agent) CheckRememberedLogin(token string) bool {
	if token == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	hash := hashToken(token)
	found := false
	for _, l := range a.readRememberedLogins() {
		// Compare every entry in constant time (see auth.VerifyPassword).
		if subtle.ConstantTimeCompare([]byte(l.Hash), []byte(hash)) == 1 {
			found = true
		}
	}
	return found
}

// ForgetLogin forgets a remembered login (logging out).
func (a *Agent) ForgetLogin(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	hash := hashToken(token)
	logins := a.readRememberedLogins()
	kept := slices.DeleteFunc(slices.Clone(logins), func(l rememberedLogin) bool { return l.Hash == hash })
	if len(kept) == len(logins) {
		return nil
	}
	return config.WriteJSON(a.paths.RememberedLoginsFile(), kept, 0o600)
}
