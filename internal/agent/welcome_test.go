package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/stations"
)

// welcomeOn is the welcome page on the guest network with house rules and
// an access code, asked again every 24 hours.
var welcomeOn = api.WelcomeSettings{
	Enabled: true, Title: "Welcome to the cabin", Message: "Enjoy your stay.",
	Terms: "No streaming 4K all night.", AccessCode: "4711", AskAgainHours: 24, Color: "#0c8a9c",
}

// newWelcomeEnv is a guest network with a guest's phone (10.87.1.23) and
// one of the owner's devices (10.87.0.23), and a clock the test moves.
func newWelcomeEnv(t *testing.T, settings api.WelcomeSettings) (*testEnv, *fakeClock) {
	t.Helper()
	e := newGuestCapableEnv(t)
	c := &fakeClock{now: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	e.agent.now = func() time.Time { return c.now }
	if err := e.agent.UpdateGuestSettings(guestOn); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, e.paths.DnsmasqLeases(),
		"1790800000 3a:1f:22:aa:bb:01 10.87.1.23 Guest-Phone *\n1790800000 3a:1f:22:aa:bb:02 10.87.0.23 Alis-iPhone *\n")
	if err := e.agent.UpdateWelcomeSettings(settings); err != nil {
		t.Fatal(err)
	}
	return e, c
}

func TestWelcome_DefaultsBeforeSaving(t *testing.T) {
	e := newClaimedEnv(t, false)
	// Settings from before the welcome page existed have none of it.
	e.agent.mu.Lock()
	e.agent.cfg.Welcome = config.WelcomeConfig{}
	e.agent.mu.Unlock()
	if s := e.agent.WelcomeSettings(); s.Enabled || s.Title != "Welcome" || s.AskAgainHours != 24 {
		t.Errorf("defaults = %+v", s)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "welcome") {
		t.Error("no welcome rules while it is off")
	}
}

func TestWelcome_SettingsAreChecked(t *testing.T) {
	e, _ := newWelcomeEnv(t, welcomeOn)
	if s := e.agent.WelcomeSettings(); !s.Enabled || s.Title != "Welcome to the cabin" || s.AccessCode != "4711" {
		t.Errorf("settings = %+v", s)
	}
	bad := map[string]func(*api.WelcomeSettings){
		"title":         func(s *api.WelcomeSettings) { s.Title = " " },
		"message":       func(s *api.WelcomeSettings) { s.Message = strings.Repeat("x", 1001) },
		"terms":         func(s *api.WelcomeSettings) { s.Terms = strings.Repeat("x", 10001) },
		"accessCode":    func(s *api.WelcomeSettings) { s.AccessCode = "123" },
		"askAgainHours": func(s *api.WelcomeSettings) { s.AskAgainHours = -1 },
		"color":         func(s *api.WelcomeSettings) { s.Color = "red; background: url(x)" },
		"logo":          func(s *api.WelcomeSettings) { s.Logo = "data:image/svg+xml;base64,PHN2Zz4=" },
		"customPage":    func(s *api.WelcomeSettings) { s.CustomPage = strings.Repeat("x", 64_001) },
	}
	for field, change := range bad {
		s := welcomeOn
		change(&s)
		var ve *config.ValidationError
		if err := e.agent.UpdateWelcomeSettings(s); !errors.As(err, &ve) || ve.Field != field {
			t.Errorf("%s: got %v", field, err)
		}
	}
	good := welcomeOn
	good.Logo = "data:image/png;base64,iVBORw0KGgo="
	if err := e.agent.UpdateWelcomeSettings(good); err != nil {
		t.Errorf("a PNG logo: %v", err)
	}
}

func TestWelcome_GuestsAcceptWithTheCode(t *testing.T) {
	e, clock := newWelcomeEnv(t, welcomeOn)

	rules := e.readFile(e.paths.FirewallRules())
	if !strings.Contains(rules, `set welcome_lans {`) || !strings.Contains(rules, `elements = { "br-guest" }`) ||
		!strings.Contains(rules, "ip daddr != { 10.87.1.1 } tcp dport 80 redirect to :2050") {
		t.Fatalf("firewall:\n%s", rules)
	}

	// The guest sees the page; the owner's device on the hotspot doesn't.
	page := e.agent.WelcomePage("10.87.1.23")
	if !page.Active || page.Accepted || page.Title != "Welcome to the cabin" || !page.NeedCode || page.Terms == "" {
		t.Errorf("guest's page = %+v", page)
	}
	if e.agent.WelcomePage("10.87.0.23").Active || e.agent.WelcomePage("10.87.1.99").Active || e.agent.WelcomePage("nope").Active {
		t.Error("only devices on the page's network see it")
	}
	if c := e.agent.Status().Guest.Clients; len(c) > 0 && c[0].Welcome != "" {
		t.Logf("guest clients: %+v", c)
	}

	// A wrong code, then the right one.
	if err := e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.1.23", Code: "1234"}); !errors.Is(err, errWrongCode) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.1.23", Code: " 4711 "}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), "elements = { 3a:1f:22:aa:bb:01 timeout 86400s }") {
		t.Error("the guest should pass for 24 hours")
	}
	if !e.agent.WelcomePage("10.87.1.23").Accepted || e.agent.WelcomeSettings().Accepted != 1 {
		t.Error("the guest has accepted")
	}
	// Kept across a restart of rbd.
	if !strings.Contains(e.readFile(e.paths.WelcomeAcceptedFile()), "3a:1f:22:aa:bb:01") {
		t.Error("accepted devices should be saved")
	}

	// A day later they are asked again.
	clock.advance(25 * time.Hour)
	if e.agent.WelcomePage("10.87.1.23").Accepted || e.agent.WelcomeSettings().Accepted != 0 {
		t.Error("the acceptance should run out")
	}

	// The hotspot never has the page: its devices are the owner's own.
	if err := e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.0.23", Code: "4711"}); err == nil {
		t.Error("the hotspot has no page")
	}
}

func TestWelcome_TooManyWrongCodesWait(t *testing.T) {
	e, clock := newWelcomeEnv(t, welcomeOn)
	for range welcomeTries {
		_ = e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.1.23", Code: "0000"})
	}
	if err := e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.1.23", Code: "4711"}); !errors.Is(err, errTooManyCodes) {
		t.Fatalf("even the right code waits: %v", err)
	}
	clock.advance(welcomeWait + time.Second)
	if err := e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.1.23", Code: "4711"}); err != nil {
		t.Fatalf("after the wait: %v", err)
	}
}

func TestWelcome_AskAgainAndForever(t *testing.T) {
	settings := welcomeOn
	settings.AccessCode, settings.AskAgainHours = "", 0
	e, _ := newWelcomeEnv(t, settings)
	if err := e.agent.AcceptWelcome(api.WelcomeAcceptRequest{IP: "10.87.1.23"}); err != nil {
		t.Fatal(err)
	}
	// Without "ask again", no timeout.
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), "elements = { 3a:1f:22:aa:bb:01 }") {
		t.Errorf("firewall:\n%s", e.readFile(e.paths.FirewallRules()))
	}
	// The Clients page shows who accepted and who still has to; the
	// owner's devices on the hotspot have no welcome state.
	guest := &stations.Station{MAC: "3a:1f:22:aa:bb:01"}
	waiting := &stations.Station{MAC: "3a:1f:22:aa:bb:03"}
	own := &stations.Station{MAC: "3a:1f:22:aa:bb:02"}
	e.agent.mu.Lock()
	e.agent.setWelcome(guest, true)
	e.agent.setWelcome(waiting, true)
	e.agent.setWelcome(own, false)
	e.agent.mu.Unlock()
	if guest.Welcome != welcomeAccepted || waiting.Welcome != welcomeWaiting || own.Welcome != "" {
		t.Errorf("welcome states: %q %q %q", guest.Welcome, waiting.Welcome, own.Welcome)
	}

	if err := e.agent.ResetWelcome("3A:1F:22:AA:BB:01"); err != nil {
		t.Fatal(err)
	}
	if e.agent.WelcomePage("10.87.1.23").Accepted {
		t.Error("asked again")
	}
	if err := e.agent.ResetWelcome("3a:1f:22:aa:bb:01"); err != nil {
		t.Error("asking again twice is fine")
	}
	if err := e.agent.ResetWelcome("nope"); err == nil {
		t.Error("not a MAC address")
	}
}

func TestWelcome_OffWhileTheGuestNetworkIs(t *testing.T) {
	e, _ := newWelcomeEnv(t, welcomeOn)
	off := guestOn
	off.Enabled = false
	if err := e.agent.UpdateGuestSettings(off); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.readFile(e.paths.FirewallRules()), "welcome_lans") {
		t.Error("no welcome rules without the guest network")
	}
	if e.agent.WelcomePage("10.87.1.23").Active {
		t.Error("no page without the guest network")
	}
	// The settings are kept for the next time.
	if !e.agent.WelcomeSettings().Enabled {
		t.Error("the page stays turned on")
	}
}

func TestWelcome_OnlyInNormalMode(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if err := e.agent.UpdateWelcomeSettings(welcomeOn); !errors.Is(err, ErrWrongMode) {
		t.Errorf("before setup: %v", err)
	}
}
