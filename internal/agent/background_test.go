package agent

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/plugin"
)

// What rbd's background loops decide in each round: when to ask GitHub
// for updates and for the plugin catalog, which Wi-Fi interface the
// certificate watcher follows, what the reset button does, and the
// plugins' health and metrics. The loops themselves only sleep and call
// these.

func TestUpdateCheckDue(t *testing.T) {
	tests := []struct {
		name       string
		checkHours int
		online     bool
		lastCheck  time.Duration // ago; 0 = never
		want       bool
	}{
		{"checked manually only", 0, true, 0, false},
		{"never checked, online", 24, true, 0, true},
		{"never checked, offline", 24, false, 0, false},
		{"checked an hour ago, daily", 24, true, time.Hour, false},
		{"checked a day ago, daily", 24, true, 25 * time.Hour, true},
		{"checked two hours ago, hourly", 1, true, 2 * time.Hour, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newClaimedEnv(t, false)
			e.agent.mu.Lock()
			e.agent.cfg.Updates.CheckHours = tc.checkHours
			e.agent.uplink.status.Connected = tc.online
			if tc.lastCheck > 0 {
				e.agent.update.lastCheck = time.Now().Add(-tc.lastCheck)
			}
			e.agent.mu.Unlock()
			if got := e.agent.updateCheckDue(); got != tc.want {
				t.Errorf("updateCheckDue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCatalogCheckDue_OnlyWithCatalogPluginsAndOnceADay(t *testing.T) {
	tests := []struct {
		name      string
		sources   []string // of the installed plugins
		checkedAt time.Duration
		want      bool
	}{
		{"no plugins", nil, 0, false},
		{"only plugins from files", []string{"file"}, 0, false},
		{"a catalog plugin, never checked", []string{"file", "aazam476/RasBeacon-plugins"}, 0, true},
		{"a catalog plugin, checked an hour ago", []string{"example/plugins"}, time.Hour, false},
		{"a catalog plugin, checked two days ago", []string{"example/plugins"}, 48 * time.Hour, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newClaimedEnv(t, false)
			e.agent.mu.Lock()
			e.agent.cfg.Plugins.Installed = map[string]config.InstalledPlugin{}
			for i, source := range tc.sources {
				e.agent.cfg.Plugins.Installed[string(rune('a'+i))] = config.InstalledPlugin{Source: source}
			}
			e.agent.plugins.catalog.loaded = true
			if tc.checkedAt > 0 {
				e.agent.plugins.catalog.CheckedAt = time.Now().Add(-tc.checkedAt)
			}
			e.agent.mu.Unlock()
			if got := e.agent.catalogCheckDue(); got != tc.want {
				t.Errorf("catalogCheckDue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServerCertWatch_FollowsTheUplinkRadioOnlyForTrustOnFirstConnect(t *testing.T) {
	e := newClaimedEnv(t, false)
	if got := e.agent.serverCertWatchInterface(); got != "" {
		t.Errorf("without Enterprise networks: %q, want none", got)
	}

	// Checked against a server name: nothing to learn on first connect.
	if err := e.agent.UpdateUplinkSettings(eduroamRequest("", "my-password")); err != nil {
		t.Fatal(err)
	}
	if got := e.agent.serverCertWatchInterface(); got != "" {
		t.Errorf("with a server name: %q, want none", got)
	}

	// Neither a CA certificate nor a server name: the watcher listens.
	req := eduroamRequest("", "my-password")
	req.Networks[0].Enterprise.ServerName = ""
	if err := e.agent.UpdateUplinkSettings(req); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	want := e.agent.wifiUplinkInterface()
	e.agent.mu.Unlock()
	if got := e.agent.serverCertWatchInterface(); got == "" || got != want {
		t.Errorf("trust on first connect: %q, want the uplink radio %q", got, want)
	}
}

func TestResetButton_ErasesEverythingAndRestarts(t *testing.T) {
	e := newClaimedEnv(t, false)
	if _, err := os.Stat(e.paths.ConfigFile()); err != nil {
		t.Fatal("the settings should exist before:", err)
	}
	e.agent.resetFromButton()
	if _, err := os.Stat(e.paths.ConfigFile()); !os.IsNotExist(err) {
		t.Errorf("the settings should be erased, got %v", err)
	}
	if !e.runner.ran("systemctl reboot") {
		t.Error("RasBeacon should restart into first-time setup")
	}
}

func TestPluginHealth_ReadsTheHealthCheckAndMetrics(t *testing.T) {
	plug := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusNoContent)
		case "/stats":
			_ = json.NewEncoder(w).Encode(map[string]any{"num_dns_queries": 1234})
		default:
			http.NotFound(w, r)
		}
	}))
	defer plug.Close()
	_, port, _ := net.SplitHostPort(plug.Listener.Addr().String())

	e := newPluginEnv(t)
	if _, err := e.installTestPlugin(writeTestPlugin(t, testPluginManifest("1.0"), nil), nil); err != nil {
		t.Fatal(err)
	}
	// The plugin answers on 127.0.0.1 here, instead of its own address.
	e.agent.mu.Lock()
	p := e.agent.cfg.Plugins.Installed["adguard-home"]
	p.Address = netip.MustParseAddr("127.0.0.1")
	e.agent.cfg.Plugins.Installed["adguard-home"] = p
	live := e.agent.plugins.live["adguard-home"]
	live.state = api.PluginRunning
	live.manifest.Health = &plugin.Health{URL: "http://{address}:" + port + "/health"}
	live.manifest.Metrics = []plugin.Metric{
		{Label: "Queries", URL: "http://{address}:" + port + "/stats", Path: "num_dns_queries"},
		{Label: "Missing", URL: "http://{address}:" + port + "/nothing", Path: "x"},
		{Label: "Elsewhere", URL: "http://192.0.2.1:" + port + "/stats", Path: "num_dns_queries"},
	}
	e.agent.mu.Unlock()

	e.agent.readPluginHealth()
	got := e.plugin("adguard-home")
	if got.Healthy == nil || !*got.Healthy {
		t.Errorf("healthy = %v, want true", got.Healthy)
	}
	values := map[string]string{}
	for _, m := range got.Metrics {
		values[m.Label] = m.Value
	}
	// A metric that can't be read, or would lead away from the plugin, shows a dash.
	if values["Queries"] != "1,234" || values["Missing"] != "" || values["Elsewhere"] != "" {
		t.Errorf("metrics = %v", got.Metrics)
	}

	// A health check that fails is unhealthy.
	plug.Close()
	e.agent.readPluginHealth()
	if got := e.plugin("adguard-home"); got.Healthy == nil || *got.Healthy {
		t.Errorf("after the plugin stopped answering: healthy = %v, want false", got.Healthy)
	}
}

func TestPluginWatchRound_DoesEachCheckOnItsRound(t *testing.T) {
	e := newPluginEnv(t)
	podmanPS := func() int {
		n := 0
		for _, c := range e.runner.commands {
			if strings.HasPrefix(c, "podman ps") {
				n++
			}
		}
		return n
	}
	before := podmanPS()
	e.agent.pluginWatchRound(1)
	if podmanPS() != before+1 {
		t.Errorf("each round looks at the containers")
	}
}
