package agent

// These tests run the agent against a temporary directory (standing in for
// the device's disk) and a fake command runner (standing in for ip, iw,
// systemctl, ...). They check what the agent WOULD do on a real device:
// which files it writes and which commands it runs.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/dhcpprobe"
	"rasbeacon/internal/paths"
)

// fakeRunner records commands instead of running them.
type fakeRunner struct {
	mu       sync.Mutex
	commands []string
	// outputs maps a command line (e.g. "iw phy phy1 info") to its output.
	outputs map[string]string
	// prefixOutputs answer every command line starting with a key.
	prefixOutputs map[string]string
	// failing makes command lines starting with one of these fail.
	failing []string
	// effect, if set, runs for every command, for commands that write
	// files (podman save).
	effect func(args []string)
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, line)
	if f.effect != nil {
		f.effect(append([]string{name}, args...))
	}
	if out, ok := f.outputs[line]; ok {
		return out, nil
	}
	for _, prefix := range f.failing {
		if strings.HasPrefix(line, prefix) {
			return "", errors.New("failed: " + line)
		}
	}
	for prefix, out := range f.prefixOutputs {
		if strings.HasPrefix(line, prefix) {
			return out, nil
		}
	}
	if strings.HasPrefix(line, "ip link show") {
		return "", errors.New("does not exist") // pretend links don't exist yet
	}
	return "", nil
}

// count reports how many commands containing text were run.
func (f *fakeRunner) count(text string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.commands {
		if strings.Contains(c, text) {
			n++
		}
	}
	return n
}

// ran reports whether a command containing text was run.
func (f *fakeRunner) ran(text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.Contains(c, text) {
			return true
		}
	}
	return false
}

// fakeTimers collects functions scheduled with afterFunc so a test can run
// them when it wants, instead of waiting in real time.
type fakeTimers struct {
	pending []func()
	delays  []time.Duration
}

func (ft *fakeTimers) afterFunc(d time.Duration, f func()) *time.Timer {
	ft.pending = append(ft.pending, f)
	ft.delays = append(ft.delays, d)
	return nil
}

// runAll runs every scheduled function (as if their time had come), the
// shortest delay first, as real time would.
func (ft *fakeTimers) runAll() {
	funcs, delays := ft.pending, ft.delays
	ft.pending, ft.delays = nil, nil
	order := make([]int, len(funcs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return delays[order[a]] < delays[order[b]] })
	for _, i := range order {
		funcs[i]()
	}
}

const iwInfoWithAP = "Wiphy phy1\n\tSupported interface modes:\n\t\t * managed\n\t\t * AP\n"

// iwInfoDualBand is the same for a radio with both bands, like the USB
// Archer T3U.
const iwInfoDualBand = iwInfoWithAP + "\tBand 1:\n\tBand 2:\n"

// testEnv is a fresh fake device.
type testEnv struct {
	t      *testing.T
	paths  paths.Paths
	runner *fakeRunner
	timers *fakeTimers
	agent  *Agent

	// runEnvs are the environment files the plugins were started with, as
	// podman read them (newPluginEnv).
	runEnvs []string

	// dhcpServer is the address the fake DHCP probe reports ("" = none).
	dhcpServer string
	probes     int

	backgroundMu sync.Mutex
	pending      []func()
}

// runBackground runs the background work scheduled so far, and what it
// schedules in turn.
func (e *testEnv) runBackground() {
	for {
		e.backgroundMu.Lock()
		work := e.pending
		e.pending = nil
		e.backgroundMu.Unlock()
		if len(work) == 0 {
			return
		}
		for _, f := range work {
			f()
		}
	}
}

// fakeDHCPProbe stands in for dhcpprobe.Probe.
func (e *testEnv) fakeDHCPProbe(iface string, timeout time.Duration) (dhcpprobe.Result, error) {
	e.probes++
	return dhcpprobe.Result{ServerFound: e.dhcpServer != "", ServerAddress: e.dhcpServer}, nil
}

// newTestEnv creates a fake device with a built-in radio (wlan0) and a USB
// Archer T3U (wlan1).
// shortTempDir is a temporary folder with a short path, removed after the
// test. Unix socket paths may be at most 104 characters on macOS (108 on
// Linux), and t.TempDir() is often longer there, so tests that make
// sockets use this.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	// Short, because plugins' power sockets are made inside it.
	p := paths.Paths{Root: shortTempDir(t)}

	// Fake /sys/class/net with two radios.
	classNet := p.SysClassNet()
	addRadio := func(iface, phy, devicePath string) {
		mustWrite(t, filepath.Join(classNet, iface, "phy80211", "name"), phy)
		device := filepath.Join(p.Root, "sys", "devices", devicePath)
		if err := os.MkdirAll(device, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(device, filepath.Join(classNet, iface, "device")); err != nil {
			t.Fatal(err)
		}
	}
	addRadio("wlan0", "phy0", "platform/mmc1/mmc1:0001:1")
	addRadio("wlan1", "phy1", "usb1/1-1/1-1:1.0")
	mustWrite(t, filepath.Join(p.Root, "sys", "devices", "usb1", "1-1", "idVendor"), "2357")
	mustWrite(t, filepath.Join(p.Root, "sys", "devices", "usb1", "1-1", "idProduct"), "012d")
	if err := os.MkdirAll(p.BootConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// The real Wi-Fi regulatory database, as the image has it.
	regulatory, err := os.ReadFile("../regdb/testdata/regulatory.db")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p.RegulatoryDB(), string(regulatory))

	runner := &fakeRunner{outputs: map[string]string{
		"iw phy phy0 info": iwInfoWithAP,
		"iw phy phy1 info": iwInfoDualBand,
	}}
	e := &testEnv{t: t, paths: p, runner: runner}
	e.agent = e.newAgent()
	return e
}

// newAgent makes an agent on e's device, with the fakes every test uses:
// e.runner for commands, fresh fake timers (in e.timers), no real waits,
// lookups or disk space, and background work that waits until a test runs
// it. So nothing touches this computer, and nothing keeps running after
// the test.
func (e *testEnv) newAgent() *Agent {
	a := New(e.paths, e.runner)
	e.timers = &fakeTimers{}
	a.afterFunc = e.timers.afterFunc
	a.usbRadioWait = 0
	a.sleep = func(time.Duration) {} // don't really wait in tests
	// Every name is at 203.0.113.7 (a documentation address), with no DNS.
	a.lookupIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.7")}, nil
	}
	// A card with 10 GB free, whatever this computer's disk holds.
	a.diskFree = func(string) (int64, error) { return 10 << 30, nil }
	a.background = func(f func()) { e.backgroundMu.Lock(); e.pending = append(e.pending, f); e.backgroundMu.Unlock() }
	a.plugins.watchEvery = 0
	a.dhcpProbe = e.fakeDHCPProbe
	return a
}

// restart is rbd starting again on the same device (after a reboot, a
// crash or an update): a new agent with the same files and fakes replaces
// e.agent (with fresh timers in e.timers), and starts.
func (e *testEnv) restart() *Agent {
	e.t.Helper()
	e.agent = e.newAgent()
	e.start()
	return e.agent
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) readFile(path string) string {
	e.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func (e *testEnv) start() {
	e.t.Helper()
	if err := e.agent.Start(); err != nil {
		e.t.Fatalf("Start: %v", err)
	}
}

// validClaim is a claim request that passes every check.
var validClaim = api.ClaimRequest{
	AdminPassword:   "tangerine-orbit-42",
	HotspotSSID:     "Ali's Travel Wi-Fi",
	HotspotPassword: "kq7m-2xvd-h8pt-rn4w",
	HotspotBand:     config.Band2GHz,
}

func TestFirstBoot_NoSetupTxt_StartsOpenSetupNetwork(t *testing.T) {
	e := newTestEnv(t)
	e.start()

	state := e.agent.SetupState()
	if state.Mode != api.ModeSetup || state.Claimed || state.SetupNetworkSecured {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.SuggestedHotspotPassword == "" {
		t.Error("setup wizard should get a suggested hotspot password")
	}

	// The USB adapter runs the hotspot and is renamed to ap0.
	if !e.runner.ran("ip link set dev wlan1 name ap0") {
		t.Errorf("USB radio should be renamed to ap0; commands: %v", e.runner.commands)
	}
	hostapd := e.readFile(e.paths.HostapdConfig())
	if strings.Contains(hostapd, "wpa=") {
		t.Error("setup network without setup.txt must be open")
	}
	if !strings.Contains(hostapd, "ssid2="+hexOf("RasBeacon-Setup-"+state.DeviceShortID)) {
		t.Error("setup network should be named RasBeacon-Setup-XXXX")
	}
	if !strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "address=/#/10.87.0.1") {
		t.Error("setup mode should send every name to the setup page")
	}
	if !e.runner.ran("nft -f") || !e.runner.ran("systemctl restart rasbeacon-hostapd.service") {
		t.Errorf("firewall and hostapd should be applied; commands: %v", e.runner.commands)
	}
	if _, err := os.Stat(e.paths.IdentityFile()); err != nil {
		t.Error("first boot should save the device identity")
	}
}

func TestFirstBoot_ValidSetupTxt_ProtectsSetupNetwork(t *testing.T) {
	e := newTestEnv(t)
	mustWrite(t, e.paths.SetupTxtFile(), "setup_password=my-setup-pass\n")
	e.start()

	if !e.agent.SetupState().SetupNetworkSecured {
		t.Error("setup network should be marked secured")
	}
	hostapd := e.readFile(e.paths.HostapdConfig())
	if !strings.Contains(hostapd, "wpa_passphrase=my-setup-pass") {
		t.Error("setup network should use the setup.txt password")
	}
}

func TestFirstBoot_BrokenSetupTxt_KeepsNetworkOffInsteadOfOpen(t *testing.T) {
	e := newTestEnv(t)
	mustWrite(t, e.paths.SetupTxtFile(), "setup_password=short\n")
	e.start()
	defer e.agent.stopLED()

	if mode := e.agent.SetupState().Mode; mode != api.ModeSetupError {
		t.Fatalf("mode = %q, want setup-error", mode)
	}
	if _, err := os.Stat(e.paths.HostapdConfig()); err == nil {
		t.Error("no hotspot config should be written when setup.txt is broken")
	}
	if !e.runner.ran("systemctl stop rasbeacon-hostapd.service") {
		t.Error("hostapd should be stopped")
	}
}

func TestSetupWindow_ClosesAfter30Minutes(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if e.timers.delays[0] != 30*time.Minute {
		t.Fatalf("setup window timer = %v, want 30m", e.timers.delays[0])
	}
	e.timers.runAll()
	if mode := e.agent.SetupState().Mode; mode != api.ModeSetupClosed {
		t.Errorf("mode = %q, want setup-closed", mode)
	}
	if _, err := e.agent.Claim(validClaim); !errors.Is(err, ErrWrongMode) {
		t.Errorf("claiming after the window closed should fail with ErrWrongMode, got %v", err)
	}
}

func TestClaim_WeakAdminPasswordIsRejectedWithField(t *testing.T) {
	e := newTestEnv(t)
	e.start()

	req := validClaim
	req.AdminPassword = "password123"
	_, err := e.agent.Claim(req)

	var validation *config.ValidationError
	if !errors.As(err, &validation) || validation.Field != "adminPassword" {
		t.Fatalf("want a validation error on adminPassword, got %v", err)
	}
	if e.agent.SetupState().Claimed {
		t.Error("device must stay unclaimed after a rejected claim")
	}
}

func TestClaim_SavesSettingsThenSwitchesToOwnersHotspot(t *testing.T) {
	e := newTestEnv(t)
	mustWrite(t, e.paths.SetupTxtFile(), "setup_password=my-setup-pass\n")
	e.start()

	resp, err := e.agent.Claim(validClaim)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if resp.HotspotSSID != validClaim.HotspotSSID || resp.AdminURL != "http://10.87.0.1/" {
		t.Errorf("unexpected response: %+v", resp)
	}

	// Saved to disk, with the secrets file private.
	var saved config.Config
	if err := config.ReadJSON(e.paths.ConfigFile(), &saved); err != nil || !saved.Claimed {
		t.Fatalf("config should be saved as claimed: %+v, %v", saved, err)
	}
	info, _ := os.Stat(e.paths.SecretsFile())
	if info.Mode().Perm() != 0o600 {
		t.Errorf("secrets file mode = %o, want 600", info.Mode().Perm())
	}
	if strings.Contains(e.readFile(e.paths.SecretsFile()), validClaim.AdminPassword) {
		t.Error("the admin password itself must never be stored, only its hash")
	}

	// The network switch happens later (after the response reaches the phone).
	e.timers.runAll()

	if mode := e.agent.SetupState().Mode; mode != api.ModeNormal {
		t.Fatalf("mode = %q, want normal", mode)
	}
	hostapd := e.readFile(e.paths.HostapdConfig())
	if !strings.Contains(hostapd, "wpa_passphrase="+validClaim.HotspotPassword) {
		t.Error("hotspot should use the owner's password")
	}
	if strings.Contains(e.readFile(e.paths.DnsmasqConfig()), "address=/#/") {
		t.Error("normal mode must not redirect every name")
	}
	if _, err := os.Stat(e.paths.SetupTxtFile()); !os.IsNotExist(err) {
		t.Error("setup.txt should be deleted after claiming")
	}
	if !e.agent.VerifyAdminPassword(validClaim.AdminPassword) || e.agent.VerifyAdminPassword("nope-nope-nope") {
		t.Error("admin password check is wrong after claim")
	}
}

func TestClaim_TwiceIsRejected(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	if _, err := e.agent.Claim(validClaim); !errors.Is(err, ErrWrongMode) {
		t.Errorf("second claim should fail with ErrWrongMode, got %v", err)
	}
}

// A second claim in the seconds before the network switch (the mode is
// still "setup") must not replace the owner's password and hotspot.
func TestClaim_SecondClaimBeforeTheSwitchIsRejected(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	intruder := validClaim
	intruder.AdminPassword = "intruder-password-123"
	intruder.HotspotSSID = "Intruder"
	intruder.HotspotPassword = "intruder-wifi-pass"
	if _, err := e.agent.Claim(intruder); !errors.Is(err, ErrWrongMode) {
		t.Errorf("a second claim before the switch = %v, want ErrWrongMode", err)
	}
	e.timers.runAll()
	if !e.agent.VerifyAdminPassword(validClaim.AdminPassword) || e.agent.VerifyAdminPassword(intruder.AdminPassword) {
		t.Error("the owner's admin password must stay")
	}
	if hostapd := e.readFile(e.paths.HostapdConfig()); !strings.Contains(hostapd, "wpa_passphrase="+validClaim.HotspotPassword) {
		t.Errorf("the owner's hotspot must stay:\n%s", hostapd)
	}
}

func TestReboot_AfterClaim_StartsInNormalMode(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()

	// Simulate a reboot: a new agent reading the same disk.
	restarted := New(e.paths, e.runner)
	restarted.afterFunc = e.timers.afterFunc
	restarted.usbRadioWait = 0
	restarted.background = func(func()) {} // no loops left running after the test
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	if mode := restarted.SetupState().Mode; mode != api.ModeNormal {
		t.Errorf("after reboot mode = %q, want normal", mode)
	}
}

func TestFactoryResetFile_WipesSettingsAtBoot(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()

	mustWrite(t, e.paths.FactoryResetFile(), "")
	restarted := New(e.paths, e.runner)
	restarted.afterFunc = e.timers.afterFunc
	restarted.usbRadioWait = 0
	restarted.background = func(func()) {} // no loops left running after the test
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	if state := restarted.SetupState(); state.Claimed || state.Mode != api.ModeSetup {
		t.Errorf("after factory reset the device should be unclaimed in setup mode, got %+v", state)
	}
	if _, err := os.Stat(e.paths.FactoryResetFile()); !os.IsNotExist(err) {
		t.Error("factory-reset file must be deleted, or every boot would reset")
	}
}

func TestUpdateHotspotSettings_ValidatesAndKeepsPasswordWhenEmpty(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()

	settings := e.agent.HotspotSettings()
	settings.Band = config.Band5GHz
	settings.Channel = 52 // DFS channel: not allowed
	if err := e.agent.UpdateHotspotSettings(settings); err == nil {
		t.Fatal("DFS channel should be rejected")
	}

	settings.Channel = 44
	settings.Password = "" // keep the current password
	if err := e.agent.UpdateHotspotSettings(settings); err != nil {
		t.Fatalf("valid update failed: %v", err)
	}
	hostapd := e.readFile(e.paths.HostapdConfig())
	if !strings.Contains(hostapd, "channel=44") || !strings.Contains(hostapd, "wpa_passphrase="+validClaim.HotspotPassword) {
		t.Errorf("hotspot config not updated correctly:\n%s", hostapd)
	}
}

func TestSSH_RequiresKeyAndOpensPortOnlyWhenOn(t *testing.T) {
	e := newTestEnv(t)
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()

	if err := e.agent.UpdateSSHSettings(api.SSHSettings{Enabled: true}); err == nil {
		t.Error("turning SSH on without a key should be rejected")
	}
	if err := e.agent.UpdateSSHSettings(api.SSHSettings{Enabled: true, AuthorizedKeys: "not a key"}); err == nil {
		t.Error("an invalid key should be rejected")
	}

	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGQ5pS6BbR0bM1vB7C3P3tYhGx7r0gq4n6qF3m3c8xQk laptop"
	if err := e.agent.UpdateSSHSettings(api.SSHSettings{Enabled: true, AuthorizedKeys: key}); err != nil {
		t.Fatalf("valid SSH settings rejected: %v", err)
	}
	if !strings.Contains(e.readFile(e.paths.FirewallRules()), "tcp dport 22 accept") {
		t.Error("SSH port should be open when SSH is on")
	}
	if !e.runner.ran("systemctl restart ssh.service") {
		t.Error("SSH server should be started")
	}
}

func TestRedact_HidesSecretsAndMACs(t *testing.T) {
	text := "password=hunter2hunter2 station 3a:1f:22:aa:bb:cc connected"
	got := Redact(text, []string{"hunter2hunter2", ""})
	if strings.Contains(got, "hunter2") || strings.Contains(got, "aa:bb:cc") {
		t.Errorf("secrets or MAC not hidden: %q", got)
	}
	if !strings.Contains(got, "3a:1f:22:xx:xx:xx") {
		t.Errorf("MAC should keep its first half: %q", got)
	}

	// IPv6 addresses built from a MAC address (EUI-64) carry it too.
	for _, tt := range []struct{ in, want string }{
		{"fe80::381f:22ff:feaa:bbcc dev br-lan", "fe80::381f:22ff:fexx:xxxx dev br-lan"},
		{"2001:db8:1:2:21f:22ff:fe0a:b", "2001:db8:1:2:21f:22ff:fexx:xxxx"},
		{"FE80::0200:00FF:FEAA:BBCC", "FE80::0200:00FF:FExx:xxxx"},
		{"fe80::ff:fe00:1 lladdr", "fe80::ff:fexx:xxxx lladdr"},
		// Private (random) addresses carry no MAC: left alone.
		{"2001:db8::8d3c:41a2:97e5:1f0b", "2001:db8::8d3c:41a2:97e5:1f0b"},
	} {
		if got := Redact(tt.in, nil); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHTTP_ClaimWithBadInputReturns400AndField(t *testing.T) {
	e := newTestEnv(t)
	e.start()

	body, _ := json.Marshal(api.ClaimRequest{AdminPassword: "short", HotspotSSID: "x", HotspotPassword: "12345678", HotspotBand: config.Band2GHz})
	rec := httptest.NewRecorder()
	e.agent.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/claim", bytes.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp api.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Field != "adminPassword" {
		t.Errorf("error should name the adminPassword field, got %+v", resp)
	}
}

// hexOf returns the hexadecimal form of s, as used by hostapd's ssid2=.
func hexOf(s string) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		out = append(out, digits[s[i]>>4], digits[s[i]&15])
	}
	return string(out)
}

// The owner's report (A1b step 7): eight VPN switch presses in two minutes
// restarted dnsmasq eight times, each dropping DNS and DHCP for a moment.
// dnsmasq now restarts only when its file changes, or when it isn't running.
func TestApplyDHCP_RestartsDnsmasqOnlyWhenSomethingChanged(t *testing.T) {
	e := newClaimedEnv(t, false)
	restarts := func() int {
		n := 0
		for _, cmd := range e.runner.commands {
			if cmd == "systemctl restart "+dnsmasqService {
				n++
			}
		}
		return n
	}
	apply := func() {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		if err := e.agent.applyDHCP(false); err != nil {
			t.Fatal(err)
		}
	}
	e.runner.outputs["systemctl is-active "+dnsmasqService] = "active\n"

	e.runner.commands = nil
	apply()
	apply()
	if got := restarts(); got != 0 {
		t.Errorf("nothing changed since start, but dnsmasq restarted %d times", got)
	}

	// A real change (metered, which dnsmasq tells devices) restarts it once.
	e.agent.mu.Lock()
	e.agent.cfg.Hotspot.Metered = !e.agent.cfg.Hotspot.Metered
	e.agent.mu.Unlock()
	apply()
	apply()
	if got := restarts(); got != 1 {
		t.Errorf("one change: dnsmasq restarted %d times, want 1", got)
	}

	// If dnsmasq stopped, the next apply starts it even without a change.
	e.runner.outputs["systemctl is-active "+dnsmasqService] = "failed\n"
	apply()
	if got := restarts(); got != 2 {
		t.Errorf("a stopped dnsmasq must be started again; restarts = %d", got)
	}
}

// Where the kernel's br_netfilter module is loaded, rbd switches off its
// filtering of bridged frames (see bridgeNetfilterOff); without it there
// is nothing to change.
func TestBridgeNetfilterOff(t *testing.T) {
	e := newClaimedEnv(t, false)
	dir := e.paths.ProcSysNetBridge()
	e.agent.bridgeNetfilterOff() // no module: no files, nothing happens
	for _, name := range []string{"bridge-nf-call-iptables", "bridge-nf-call-ip6tables", "bridge-nf-call-arptables"} {
		mustWrite(t, filepath.Join(dir, name), "1\n")
	}
	e.agent.bridgeNetfilterOff()
	for _, name := range []string{"bridge-nf-call-iptables", "bridge-nf-call-ip6tables", "bridge-nf-call-arptables"} {
		if got := e.readFile(filepath.Join(dir, name)); strings.TrimSpace(got) != "0" {
			t.Errorf("%s = %q, want 0", name, got)
		}
	}
}
