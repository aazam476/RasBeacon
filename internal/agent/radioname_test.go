package agent

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// A radio's name is saved by its own address, shown in the status, and
// goes back to the default when emptied; nothing restarts.
func TestRadioName_SavedShownAndCleared(t *testing.T) {
	e := newRadiosEnv(t)
	nameOf := func(mac string) string {
		for _, r := range e.agent.Status().Radios {
			if r.MAC == mac {
				return r.Name
			}
		}
		t.Fatalf("no radio %s", mac)
		return ""
	}

	restarts := e.runner.count("systemctl restart rbd.service")
	if err := e.agent.SetRadioName(strings.ToUpper(usbMAC), "  Blue adapter "); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(usbMAC); got != "Blue adapter" {
		t.Errorf("name = %q, want Blue adapter", got)
	}
	if got := nameOf(builtInMAC); got != "" {
		t.Errorf("the built-in radio has no name of its own, got %q", got)
	}
	// The name is only for the pages: the detected radios don't carry it.
	if r := e.agent.radioByMAC(usbMAC); r.Name != "" {
		t.Errorf("a.radios should stay as detected, got %q", r.Name)
	}
	var saved config.Config
	if err := config.ReadJSON(e.paths.ConfigFile(), &saved); err != nil || saved.Radios.Names[usbMAC] != "Blue adapter" {
		t.Errorf("the name should be saved: %+v %v", saved.Radios, err)
	}

	if err := e.agent.SetRadioName(usbMAC, ""); err != nil {
		t.Fatal(err)
	}
	if got := nameOf(usbMAC); got != "" {
		t.Errorf("an empty name goes back to the default, got %q", got)
	}
	// Naming never restarts rbd (a change of roles does).
	e.timers.runAll()
	if got := e.runner.count("systemctl restart rbd.service"); got != restarts {
		t.Errorf("renaming restarted rbd (%d restarts, want %d)", got, restarts)
	}
}

// Names follow the same rules as a device's, and only radios that are
// plugged in can be named.
func TestRadioName_Rules(t *testing.T) {
	e := newRadiosEnv(t)
	var invalid *config.ValidationError
	cases := []struct{ mac, name, field string }{
		{usbMAC, strings.Repeat("x", 41), "name"},
		{usbMAC, "tab\there", "name"},
		{"not a mac", "Blue", "mac"},
		{"02:00:00:00:00:09", "Gone", "name"},
	}
	for _, c := range cases {
		if err := e.agent.SetRadioName(c.mac, c.name); !errors.As(err, &invalid) || invalid.Field != c.field {
			t.Errorf("%q %q: got %v, want a problem with %s", c.mac, c.name, err, c.field)
		}
	}
	// Forty characters are fine.
	if err := e.agent.SetRadioName(usbMAC, strings.Repeat("é", 40)); err != nil {
		t.Errorf("40 characters: %v", err)
	}
}

// Naming waits while RasBeacon isn't running normally (as in setup).
func TestRadioName_WrongMode(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if err := e.agent.SetRadioName(builtInMAC, "Inside"); !errors.Is(err, ErrWrongMode) {
		t.Errorf("got %v, want ErrWrongMode", err)
	}
}

// The route reaches SetRadioName and refuses a body it can't read.
func TestRadioName_Route(t *testing.T) {
	e := newRadiosEnv(t)
	call := func(body any) int {
		status, _ := e.call("POST", "/settings/radios/name", body)
		return status
	}
	if got := call(api.RadioNameRequest{MAC: usbMAC, Name: "Blue"}); got != http.StatusNoContent {
		t.Errorf("rename = %d, want 204", got)
	}
	if got := call(`{"surprise": true}`); got != http.StatusBadRequest {
		t.Errorf("unknown field = %d, want 400", got)
	}
}
