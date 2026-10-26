package agent

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/update"
	"rasbeacon/internal/version"
)

// sparseImage makes a sparse image holding data (a multiple of 16 bytes)
// as one raw chunk with block size 16.
func sparseImage(data []byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	for _, v := range []any{uint32(0xED26FF3A), uint16(1), uint16(0), uint16(28), uint16(12),
		uint32(16), uint32(len(data) / 16), uint32(1), uint32(0)} {
		binary.Write(&b, le, v)
	}
	for _, v := range []any{uint16(0xCAC1), uint16(0), uint32(len(data) / 16), uint32(12 + len(data))} {
		binary.Write(&b, le, v)
	}
	b.Write(data)
	return b.Bytes()
}

// testBundle is an (uncompressed) bundle with boot and system images.
func testBundle(boot, system string) []byte {
	var b bytes.Buffer
	t := tar.NewWriter(&b)
	for name, content := range map[string]string{"boot": boot, "system": system} {
		image := sparseImage([]byte(content))
		t.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(image))})
		t.Write(image)
	}
	t.Close()
	return b.Bytes()
}

// updateEnv is a claimed device with an update key, a board file, and
// fake "other slot" partitions (64-byte files).
func newUpdateEnv(t *testing.T) (*testEnv, ed25519.PrivateKey) {
	t.Helper()
	e := newClaimedEnv(t, false)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	mustWrite(t, filepath.Join(e.paths.UpdateKeysDir(), "release.pub"), base64.StdEncoding.EncodeToString(public))
	mustWrite(t, e.paths.BoardFile(), "pi4\n")
	for _, part := range []string{"boot", "system"} {
		mustWrite(t, e.paths.SlotDevice("other", part), strings.Repeat("o", 64))
	}
	// The test bundles are not compressed.
	e.agent.decompress = func(r io.Reader) (io.Reader, func() error, error) {
		return r, func() error { return nil }, nil
	}
	return e, private
}

func updateFile(t *testing.T, key ed25519.PrivateKey, board string, bundle []byte) []byte {
	t.Helper()
	return versionedUpdateFile(t, key, board, "v9.9.9", "", bundle)
}

// versionedUpdateFile is an update file for newVersion that versions from
// minFrom on can install.
func versionedUpdateFile(t *testing.T, key ed25519.PrivateKey, board, newVersion, minFrom string, bundle []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := update.Pack(&out, bytes.NewReader(bundle), board, newVersion, minFrom, key); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// runningVersion pretends RasBeacon v is running, for one test.
func runningVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

func TestUpdate_RefusesOlderVersions(t *testing.T) {
	e, key := newUpdateEnv(t)
	runningVersion(t, "v1.2.0")
	file := versionedUpdateFile(t, key, "pi4", "v1.1.0", "", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	err := e.agent.InstallUpdate(bytes.NewReader(file))
	var verr *config.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Message, "older") {
		t.Fatalf("got %v, want a refusal", err)
	}
	if e.readFile(e.paths.SlotDevice("other", "system")) != strings.Repeat("o", 64) || e.runner.ran("tryboot") {
		t.Error("nothing may be written or restarted")
	}
	if e.agent.Status().Update.Reflash != nil {
		t.Error("an older version is no reason to flash")
	}
}

func TestUpdate_TooNewAnUpdateAsksForAFreshInstall(t *testing.T) {
	e, key := newUpdateEnv(t)
	runningVersion(t, "v1.0.0")
	bundle := testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64))
	err := e.agent.InstallUpdate(bytes.NewReader(versionedUpdateFile(t, key, "pi4", "v2.0.0", "v1.5.0", bundle)))
	var verr *config.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Message, "Back up your settings") {
		t.Fatalf("got %v, want a refusal that explains the way", err)
	}
	if e.readFile(e.paths.SlotDevice("other", "system")) != strings.Repeat("o", 64) || e.runner.ran("tryboot") {
		t.Error("nothing may be written or restarted")
	}
	reflash := e.agent.Status().Update.Reflash
	if reflash == nil || reflash.Version != "v2.0.0" || reflash.MinFromVersion != "v1.5.0" {
		t.Errorf("reflash = %+v", reflash)
	}

	// A version this one can install clears it.
	if err := e.agent.InstallUpdate(bytes.NewReader(versionedUpdateFile(t, key, "pi4", "v1.5.0", "v1.0.0", bundle))); err != nil {
		t.Fatal(err)
	}
	if e.agent.Status().Update.Reflash != nil {
		t.Error("the refusal should be forgotten after an update that worked")
	}
}

func TestUpdateStatus_OnlineCheckOffersOnlyNewerVersions(t *testing.T) {
	e, _ := newUpdateEnv(t)
	runningVersion(t, "v1.2.0")
	status := func(latest latestRelease) api.UpdateStatus {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		e.agent.update.latest = latest
		return e.agent.updateStatus()
	}
	if s := status(latestRelease{TagName: "v1.1.0"}); s.LatestVersion != "" {
		t.Errorf("an older release must not be offered: %+v", s)
	}
	if s := status(latestRelease{TagName: "v1.3.0", minFromVersion: "v1.0.0"}); s.LatestVersion != "v1.3.0" || s.Reflash != nil {
		t.Errorf("a release this version can install: %+v", s)
	}
	s := status(latestRelease{TagName: "v2.0.0", HTMLURL: "https://example.com/v2", minFromVersion: "v1.5.0"})
	if s.LatestVersion != "v2.0.0" || s.Reflash == nil || s.Reflash.MinFromVersion != "v1.5.0" || s.Reflash.URL == "" {
		t.Errorf("a release that needs a fresh install: %+v", s)
	}

	// A step on the way to a newer release names that release too.
	e.agent.mu.Lock()
	e.agent.update.newest = "v1.4.0"
	e.agent.mu.Unlock()
	if s := status(latestRelease{TagName: "v1.3.0"}); s.LatestVersion != "v1.3.0" || s.NewerVersion != "v1.4.0" {
		t.Errorf("a step: %+v", s)
	}
	if s := status(latestRelease{TagName: "v1.4.0"}); s.NewerVersion != "" {
		t.Errorf("the newest release is no step: %+v", s)
	}
}

func TestUpdate_InstallWritesTheOtherSlotAndTriesIt(t *testing.T) {
	e, key := newUpdateEnv(t)
	boot, system := strings.Repeat("B", 32), strings.Repeat("S", 64)
	if err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "pi4", testBundle(boot, system)))); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(e.paths.SlotDevice("other", "system")); got != system {
		t.Errorf("system partition = %q", got)
	}
	if got := e.readFile(e.paths.SlotDevice("other", "boot")); !strings.HasPrefix(got, boot) {
		t.Errorf("boot partition = %q", got)
	}
	e.timers.runAll()
	// systemd 257 refuses "systemctl reboot '0 tryboot'" (Too many
	// arguments): the argument must go in --reboot-argument.
	if !e.runner.ran("systemctl reboot --reboot-argument=0 tryboot") {
		t.Errorf("RasBeacon should restart into the new slot on trial; ran %q", e.runner.commands)
	}
	if e.runner.ran("systemctl reboot 0 tryboot") {
		t.Error("the argument must not be passed as a plain extra word")
	}
	var pending pendingUpdate
	if err := config.ReadJSON(e.paths.UpdatePendingFile(), &pending); err != nil || !pending.Restarting {
		t.Errorf("the pending update should be remembered as restarting: %+v, %v", pending, err)
	}
	if e.agent.Status().Update.RestartPending {
		t.Error("nothing waits for a restart once it was asked for")
	}
}

func TestUpdate_AFailedRestartIsShownAndCanBeTriedAgain(t *testing.T) {
	e, key := newUpdateEnv(t)
	e.runner.failing = append(e.runner.failing, "systemctl reboot")
	if err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64))))); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()

	// The failure is shown with its reason, and the update keeps waiting.
	update := e.agent.Status().Update
	if !update.RestartPending || !strings.Contains(update.Message, "couldn't restart into it") || !strings.Contains(update.Message, "failed: systemctl reboot") {
		t.Errorf("status after a failed restart: %+v", update)
	}
	var pending pendingUpdate
	if err := config.ReadJSON(e.paths.UpdatePendingFile(), &pending); err != nil || pending.Restarting || pending.Version != "v9.9.9" {
		t.Errorf("the update should wait, not marked as restarting: %+v, %v", pending, err)
	}

	// "Restart to finish the update" tries again, and this time it works.
	e.runner.failing = nil
	if err := e.agent.RestartIntoUpdate(); err != nil {
		t.Fatal(err)
	}
	e.timers.runAll()
	if update := e.agent.Status().Update; update.RestartPending {
		t.Errorf("the restart went through, so nothing waits: %+v", update)
	}
	if err := config.ReadJSON(e.paths.UpdatePendingFile(), &pending); err != nil || !pending.Restarting {
		t.Errorf("the restart should be noted: %+v, %v", pending, err)
	}
}

func TestUpdate_RestartingNeedsAnInstalledUpdate(t *testing.T) {
	e, _ := newUpdateEnv(t)
	var verr *config.ValidationError
	if err := e.agent.RestartIntoUpdate(); !errors.As(err, &verr) {
		t.Errorf("got %v, want a refusal when no update waits", err)
	}
	// On trial, the update is already running.
	mustWrite(t, e.paths.UpdatePendingFile(), `{"version":"v9.9.9","fromVersion":"dev"}`)
	e.agent.mu.Lock()
	e.agent.update.trying = true
	e.agent.mu.Unlock()
	if err := e.agent.RestartIntoUpdate(); !errors.As(err, &verr) {
		t.Errorf("got %v, want a refusal on trial", err)
	}
	if err := e.agent.restartIntoUpdate(); err != nil {
		t.Errorf("the restart itself reads the pending file: %v", err)
	}
	_ = os.Remove(e.paths.UpdatePendingFile())
	if err := e.agent.restartIntoUpdate(); err == nil {
		t.Error("without a pending file there is nothing to restart into")
	}
}

func TestUpdate_AnUpdateThatNeverRestartedWaitsAtTheNextStart(t *testing.T) {
	e := newTestEnv(t)
	// Installed, but rbd or the power stopped before the restart was asked for.
	mustWrite(t, e.paths.UpdatePendingFile(), `{"version":"v9.9.9","fromVersion":"dev"}`)
	e.start()
	update := e.agent.Status().Update
	if !update.RestartPending || !strings.Contains(update.Message, "Restart RasBeacon to finish installing it") {
		t.Errorf("status = %+v", update)
	}
	if _, err := os.Stat(e.paths.UpdatePendingFile()); err != nil {
		t.Error("the waiting update must be kept")
	}
}

// While the signing key is being changed, the image holds the old key and the
// new one (docs/releasing.md), and an update signed with either must install.
func TestUpdate_AcceptsEveryKeyInTheImage(t *testing.T) {
	e, oldKey := newUpdateEnv(t)
	newPublic, newKey, _ := ed25519.GenerateKey(rand.Reader)
	mustWrite(t, filepath.Join(e.paths.UpdateKeysDir(), "release-2.pub"), base64.StdEncoding.EncodeToString(newPublic))
	// A broken key file is skipped, not a reason to refuse every update.
	mustWrite(t, filepath.Join(e.paths.UpdateKeysDir(), "broken.pub"), "not a key")
	if got := len(e.agent.trustedUpdateKeys()); got != 2 {
		t.Fatalf("trusted keys = %d, want 2", got)
	}

	for name, key := range map[string]ed25519.PrivateKey{"old key": oldKey, "new key": newKey} {
		t.Run(name, func(t *testing.T) {
			system := strings.Repeat(strings.ToUpper(name[:1]), 64) // "O…" or "N…", unlike the slot's "o…"
			file := updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), system))
			if err := e.agent.InstallUpdate(bytes.NewReader(file)); err != nil {
				t.Fatal(err)
			}
			if got := e.readFile(e.paths.SlotDevice("other", "system")); got != system {
				t.Errorf("system partition = %q", got)
			}
		})
	}
}

func TestUpdate_RefusesBadFilesWithoutTouchingTheSlot(t *testing.T) {
	e, key := newUpdateEnv(t)
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	bundle := testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64))

	tests := map[string][]byte{
		"foreign signature": updateFile(t, otherKey, "pi4", bundle),
		"other board":       updateFile(t, key, "pi5", bundle),
		"not an update":     []byte("hello"),
	}
	for name, file := range tests {
		t.Run(name, func(t *testing.T) {
			err := e.agent.InstallUpdate(bytes.NewReader(file))
			var verr *config.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("got %v, want a refusal", err)
			}
			if got := e.readFile(e.paths.SlotDevice("other", "system")); got != strings.Repeat("o", 64) {
				t.Error("nothing may be written for a refused update")
			}
		})
	}
	if e.runner.ran("tryboot") {
		t.Error("a refused update must never be started")
	}
}

func TestUpdate_TamperedBundleIsNotStarted(t *testing.T) {
	e, key := newUpdateEnv(t)
	file := updateFile(t, key, "pi4", testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	tampered := bytes.Replace(file, []byte(strings.Repeat("S", 64)), []byte(strings.Repeat("X", 64)), 1)
	if err := e.agent.InstallUpdate(bytes.NewReader(tampered)); err == nil {
		t.Fatal("a changed bundle must be refused")
	}
	e.timers.runAll()
	if e.runner.ran("tryboot") {
		t.Error("a changed bundle must never be started")
	}
	if !strings.Contains(e.agent.Status().Update.Message, "checksum") {
		t.Errorf("message = %q", e.agent.Status().Update.Message)
	}
}

func TestUpdate_TrialIsConfirmedAfterANormalStart(t *testing.T) {
	e := newTestEnv(t)
	mustWrite(t, e.paths.TrybootFlag(), "\x00\x00\x00\x01")
	e.runner.outputs["rpi-slot-tryboot"] = "[all]\ntryboot_a_b=1\nboot_partition=3\n[tryboot]\nboot_partition=2\n"
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	// Simulate the reboot into the claimed device on trial.
	restarted := New(e.paths, e.runner)
	timers := &fakeTimers{}
	restarted.afterFunc, restarted.usbRadioWait = timers.afterFunc, 0
	restarted.background = func(func()) {} // no loops left running after the test
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	timers.runAll() // confirmation (after a minute) and the fallback (5 minutes)
	if got := e.readFile(e.paths.AutobootFile()); !strings.Contains(got, "boot_partition=3") {
		t.Errorf("autoboot.txt = %q, want the new slot as default", got)
	}
	for _, c := range e.runner.commands {
		if strings.Contains(c, "reboot") {
			t.Errorf("a confirmed update must not be rolled back (ran %q)", c)
		}
	}
}

// Without the web UI or the hotspot, the owner couldn't reach RasBeacon to
// go back, so the update isn't confirmed and the old system comes back.
func TestUpdate_TrialIsntConfirmedWithoutTheWebUI(t *testing.T) {
	e := newTestEnv(t)
	mustWrite(t, e.paths.TrybootFlag(), "\x00\x00\x00\x01")
	e.runner.outputs["rpi-slot-tryboot"] = "[all]\ntryboot_a_b=1\nboot_partition=3\n[tryboot]\nboot_partition=2\n"
	e.start()
	if _, err := e.agent.Claim(validClaim); err != nil {
		t.Fatal(err)
	}
	e.runner.failing = []string{"systemctl is-active rbweb.service"}
	restarted := New(e.paths, e.runner)
	timers := &fakeTimers{}
	restarted.afterFunc, restarted.usbRadioWait = timers.afterFunc, 0
	restarted.background = func(func()) {} // no loops left running after the test
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	// Each round runs what is due: the first check, then the checks again.
	for range 20 {
		timers.runAll()
	}
	if _, err := os.Stat(e.paths.AutobootFile()); err == nil {
		t.Error("the update must not be confirmed while the web UI isn't running")
	}
	if checks := restarted.update.confirmChecks; checks < 2 || checks > 16 {
		t.Errorf("checked %d times, want a few retries before the rollback", checks)
	}
	if !e.runner.ran("systemctl reboot") {
		t.Error("without confirmation, RasBeacon goes back to the old system")
	}
}

func TestUpdate_FailedTrialIsReported(t *testing.T) {
	e := newTestEnv(t)
	// An update was installed, but this start is NOT a trial: the new slot
	// didn't come up, and the bootloader went back to the old one.
	mustWrite(t, e.paths.UpdatePendingFile(), `{"version":"v9.9.9","fromVersion":"dev","restarting":true}`)
	e.start()
	update := e.agent.Status().Update
	if !strings.Contains(update.Message, "didn't start correctly") || update.RestartPending {
		t.Errorf("status = %+v", update)
	}
	if _, err := os.Stat(e.paths.UpdatePendingFile()); err == nil {
		t.Error("a failed trial is over: the pending file goes")
	}
}

func TestUpdate_RealZstdDecompression(t *testing.T) {
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd is not installed")
	}
	e, key := newUpdateEnv(t)
	e.agent.decompress = zstdDecompress
	cmd := exec.Command("zstd", "-q", "-c")
	cmd.Stdin = bytes.NewReader(testBundle(strings.Repeat("B", 32), strings.Repeat("S", 64)))
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "pi4", compressed))); err != nil {
		t.Fatal(err)
	}
	if got := e.readFile(e.paths.SlotDevice("other", "system")); got != strings.Repeat("S", 64) {
		t.Errorf("system partition = %q", got)
	}
}

// releaseServer is a fake GitHub with a repository's releases: each one
// given as its tag and, if not "", the minFromVersion of its .update.json.
func releaseServer(t *testing.T, releases string, minFrom map[string]string) *httptest.Server {
	t.Helper()
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/rasbeacon/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") == "" {
			t.Error("the check should ask for a page of releases")
		}
		w.Write([]byte(strings.ReplaceAll(releases, "SERVER", serverURL)))
	})
	mux.HandleFunc("/info/", func(w http.ResponseWriter, r *http.Request) {
		tag := strings.TrimPrefix(r.URL.Path, "/info/")
		if tag == "broken" {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"version":%q,"minFromVersion":%q}`, tag, minFrom[tag])
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	serverURL = server.URL
	return server
}

// release is one release in GitHub's answer, with an .update.json file.
func release(tag, extra string) string {
	return fmt.Sprintf(`{"tag_name":%q,"html_url":"https://github.com/owner/rasbeacon/releases/tag/%s",%s
		"assets":[{"name":"rasbeacon-%s.img.xz","browser_download_url":"SERVER/img"},
		{"name":"rasbeacon-%s.update.json","browser_download_url":"SERVER/info/%s"}]}`, tag, tag, extra, tag, tag, tag)
}

func TestFetchUpdateRelease_StepsThroughVersionsThatNeedANewerStart(t *testing.T) {
	// v0.3.0 needs v0.2.0 (say, a new signing key), and v0.2.1 needs
	// nothing; v0.4.0-beta.1 is a pre-release and v0.5.0 a draft.
	releases := "[" + strings.Join([]string{
		release("v0.2.1", ""),
		release("v0.4.0-beta.1", `"prerelease":true,`),
		release("v0.5.0", `"draft":true,`),
		release("v0.3.0", ""),
		release("v0.1.0", ""),
		release("v0.2.0", ""),
	}, ",") + "]"
	server := releaseServer(t, releases, map[string]string{"v0.3.0": "v0.2.0"})
	fetch := func(running string) (latestRelease, string) {
		t.Helper()
		got, newest, err := fetchUpdateRelease(server.Client(), server.URL, "owner/rasbeacon", running)
		if err != nil {
			t.Fatal(err)
		}
		return got, newest
	}

	// v0.1.0 can't install v0.3.0 yet: it is offered v0.2.1 first.
	got, newest := fetch("v0.1.0")
	if got.TagName != "v0.2.1" || newest != "v0.3.0" || !strings.HasSuffix(got.HTMLURL, "v0.2.1") {
		t.Errorf("from v0.1.0: offered %q, newest %q", got.TagName, newest)
	}
	// Once on v0.2.1, the next check offers v0.3.0.
	if got, newest := fetch("v0.2.1"); got.TagName != "v0.3.0" || newest != "v0.3.0" || got.minFromVersion != "v0.2.0" {
		t.Errorf("from v0.2.1: offered %+v, newest %q", got, newest)
	}
	// Nothing newer than the newest release.
	if got, newest := fetch("v0.3.0"); got.TagName != "" || newest != "" {
		t.Errorf("from v0.3.0: offered %q, newest %q", got.TagName, newest)
	}
}

func TestFetchUpdateRelease_OffersTheNewestWhenNoneCanBeInstalled(t *testing.T) {
	releases := "[" + release("v2.0.0", "") + "," + release("v1.5.0", "") + "]"
	server := releaseServer(t, releases, map[string]string{"v2.0.0": "v1.5.0", "v1.5.0": "v1.5.0"})
	got, newest, err := fetchUpdateRelease(server.Client(), server.URL, "owner/rasbeacon", "v1.0.0")
	if err != nil || got.TagName != "v2.0.0" || newest != "v2.0.0" || got.minFromVersion != "v1.5.0" {
		t.Errorf("got %+v, %q, %v; want v2.0.0, which needs a fresh install", got, newest, err)
	}
}

func TestFetchUpdateRelease_UnreadableUpdateInfoIsFoundOutWhenInstalling(t *testing.T) {
	// Without readable update info, the release is offered: installing its
	// file says whether it fits (TestUpdate_TooNewAnUpdateAsksForAFreshInstall).
	releases := `[{"tag_name":"v1.1.0","assets":[{"name":"x.update.json","browser_download_url":"SERVER/info/broken"}]},
		{"tag_name":"v1.0.5","assets":[{"name":"x.update.json","browser_download_url":"SERVER/info/v1.0.4"}]},
		{"tag_name":"v1.0.1"}]`
	server := releaseServer(t, releases, map[string]string{"v1.0.4": "v9.0.0"})
	got, newest, err := fetchUpdateRelease(server.Client(), server.URL, "owner/rasbeacon", "v1.0.0")
	if err != nil || got.TagName != "v1.1.0" || got.minFromVersion != "" || newest != "v1.1.0" {
		t.Errorf("got %+v, %q, %v", got, newest, err)
	}
	// Update info that names another version is ignored too.
	if got := readMinFromVersion(server.Client(), latestRelease{TagName: "v1.0.5", Assets: []releaseAsset{{Name: "x.update.json", URL: server.URL + "/info/v1.0.4"}}}); got != "" {
		t.Errorf("update info for another version: minFrom %q", got)
	}
}

func TestFetchUpdateRelease_Errors(t *testing.T) {
	server := releaseServer(t, "not json", nil)
	if _, _, err := fetchUpdateRelease(server.Client(), server.URL, "owner/rasbeacon", "v1.0.0"); err == nil {
		t.Error("an unreadable answer should be an error")
	}
	if _, _, err := fetchUpdateRelease(server.Client(), server.URL, "owner/missing", "v1.0.0"); err == nil {
		t.Error("a missing repository should be an error")
	}
	if _, _, err := fetchUpdateRelease(server.Client(), "http://127.0.0.1:1", "owner/rasbeacon", "v1.0.0"); err == nil {
		t.Error("an unreachable GitHub should be an error")
	}
	if _, _, err := fetchUpdateRelease(server.Client(), "http://bad host", "owner/rasbeacon", "v1.0.0"); err == nil {
		t.Error("a bad address should be an error")
	}
}

func TestUpdate_ThePi3ImageIsFlashedInstead(t *testing.T) {
	e, key := newUpdateEnv(t)
	mustWrite(t, e.paths.BoardFile(), "rpi3\n")
	e.agent.mu.Lock()
	s := e.agent.updateStatus()
	e.agent.mu.Unlock()
	if s.CanInstall || !s.SingleSystem || s.Board != "rpi3" {
		t.Errorf("status = %+v: the Pi Zero 2 W/3B/3B+ image has one system copy, so it can't install updates", s)
	}
	err := e.agent.InstallUpdate(bytes.NewReader(updateFile(t, key, "rpi3", testBundle("B", "S"))))
	var ve *config.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Message, "flashed") {
		t.Errorf("got %v", err)
	}
	if e.readFile(e.paths.SlotDevice("other", "system")) != strings.Repeat("o", 64) || e.runner.ran("tryboot") {
		t.Error("nothing may be written or restarted")
	}
}

func TestStorage_ThePi3ImageHasOneSystemPartition(t *testing.T) {
	e, _ := newUpdateEnv(t)
	mustWrite(t, e.paths.BoardFile(), "rpi3\n")
	// /dev/disk/by-slot/{boot,system} point at the card's partitions; the
	// boot partition is 256 MB.
	for part, dev := range map[string]string{"boot": "mmcblk0p1", "system": "mmcblk0p2"} {
		target := filepath.Join(e.paths.Root, "dev", dev)
		mustWrite(t, target, "")
		if err := os.MkdirAll(filepath.Dir(e.paths.SingleSystemDevice(part)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, e.paths.SingleSystemDevice(part)); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(e.paths.SysClassBlock(), "mmcblk0p1", "size"), "524288\n")
	mustWrite(t, filepath.Join(e.paths.PersistentDir(), "rasbeacon", "settings"), strings.Repeat("x", 4096))

	storage := func() api.StorageInfo {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		return e.agent.storageInfo()
	}
	// /persistent is added up in the background, outside rbd's lock: the
	// first answer doesn't wait for it.
	if s := storage(); s.UserGB != 0 {
		t.Errorf("before the first measurement: %+v", s)
	}
	e.runBackground()
	s := storage()
	if s.Medium != "microSD card" || !strings.HasPrefix(s.Filesystem, "Writable system") {
		t.Errorf("storage = %+v", s)
	}
	if s.FirmwareGB < 0.268 || s.UserGB <= 0 || s.TotalGB < s.FirmwareGB+s.UserGB {
		t.Errorf("storage = %+v: RasBeacon is the boot partition and the system files, settings what /persistent holds", s)
	}
	// Not measured again for 10 minutes.
	storage()
	e.backgroundMu.Lock()
	queued := len(e.pending)
	e.backgroundMu.Unlock()
	if queued != 0 {
		t.Errorf("%d measurements queued right after one, want none for 10 minutes", queued)
	}
}

func TestUpdateCheck_AnnouncesANewVersionOnce(t *testing.T) {
	e := newClaimedEnv(t, false)
	runningVersion(t, "v1.0.0")
	oldRepo, oldAPI := version.Repository, githubAPI
	t.Cleanup(func() { version.Repository, githubAPI = oldRepo, oldAPI })
	version.Repository = "owner/rasbeacon"

	var announced []string
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/rasbeacon/releases", func(w http.ResponseWriter, r *http.Request) {
		// v1.2.0 can only be installed over v1.1.0, so v1.1.0 is announced.
		w.Write([]byte(`[{"tag_name":"v1.2.0","assets":[{"name":"rasbeacon-v1.2.0.update.json","browser_download_url":"` + serverURL + `/info"}]},
			{"tag_name":"v1.1.0","html_url":"https://github.com/owner/rasbeacon/releases/tag/v1.1.0"}]`))
	})
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"v1.2.0","minFromVersion":"v1.1.0"}`))
	})
	mux.HandleFunc("/topic", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		announced = append(announced, r.Header.Get("Title")+": "+string(body))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL
	githubAPI = server.URL

	if err := e.agent.UpdateUpdateSettings(api.UpdateSettings{CheckHours: 24, NotifyURL: server.URL + "/topic"}); err != nil {
		t.Fatal(err)
	}
	if err := e.agent.UpdateUpdateSettings(api.UpdateSettings{CheckHours: 5}); err == nil {
		t.Error("only the listed schedules may be chosen")
	}
	status, err := e.agent.CheckForUpdateNow()
	if err != nil || status.LatestVersion != "v1.1.0" || status.NewerVersion != "v1.2.0" || status.LastChecked == "" || status.CheckHours != 24 {
		t.Fatalf("status %+v, %v", status, err)
	}
	if _, err := e.agent.CheckForUpdateNow(); err != nil {
		t.Fatal(err)
	}
	if len(announced) != 1 || !strings.Contains(announced[0], "RasBeacon v1.1.0") || !strings.Contains(announced[0], "v1.2.0 can only be installed over it") {
		t.Errorf("announced %q, want the new version once", announced)
	}
	if err := e.agent.SendTestNotification(""); err != nil || len(announced) != 2 {
		t.Errorf("test message: %v, %q", err, announced)
	}
}
