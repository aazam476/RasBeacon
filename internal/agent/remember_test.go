package agent

import (
	"strings"
	"testing"
	"time"
)

func TestRememberedLogins(t *testing.T) {
	e := newClaimedEnv(t, false)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }

	token, expires, err := e.agent.RememberLogin()
	if err != nil || len(token) < 40 || !expires.Equal(c.now.Add(rememberTime)) {
		t.Fatalf("got %q %v %v", token, expires, err)
	}
	if strings.Contains(e.readFile(e.paths.RememberedLoginsFile()), token) {
		t.Error("only the token's hash may be stored")
	}
	if !e.agent.CheckRememberedLogin(token) || e.agent.CheckRememberedLogin("wrong") || e.agent.CheckRememberedLogin("") {
		t.Error("only the real token logs in")
	}
	other, _, _ := e.agent.RememberLogin()

	// Logging out forgets only that browser.
	if err := e.agent.ForgetLogin(token); err != nil {
		t.Fatal(err)
	}
	if e.agent.CheckRememberedLogin(token) || !e.agent.CheckRememberedLogin(other) {
		t.Error("forgetting one login must keep the others")
	}

	// After rememberTime, the password is needed again.
	c.advance(rememberTime)
	if e.agent.CheckRememberedLogin(other) {
		t.Error("an expired login must not log in")
	}

	// The list stays short.
	for range maxRememberedLogins + 5 {
		if _, _, err := e.agent.RememberLogin(); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(e.agent.readRememberedLogins()); n != maxRememberedLogins {
		t.Errorf("%d remembered logins", n)
	}
}
