package agent

// This file installs updates and confirms them, using the two copies
// ("slots", A and B) of the system on the SD card:
//
//  1. InstallUpdate checks the update file's signature (internal/update),
//     writes the new boot and system partitions into the slot that is not
//     running, and checks the SHA-256 of everything it wrote. The running
//     system is never touched.
//  2. RasBeacon restarts with "tryboot": the bootloader starts the other
//     slot once, on trial. (A power cut or a crash during the trial simply
//     means the next start is the old slot again.)
//  3. In the new slot, if rbd has been running normally for a minute,
//     confirmUpdate makes the new slot the default (autoboot.txt).
//     Otherwise, after 5 minutes rbd restarts the device, which goes back
//     to the old slot.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/update"
	"rasbeacon/internal/version"
)

const (
	// confirmAfter: how long the new slot must run normally before it is
	// made the default.
	confirmAfter = time.Minute
	// rollbackAfter: if the new slot isn't confirmed by then, restart into
	// the old one.
	rollbackAfter = 5 * time.Minute
)

// ErrUpdateBusy means another update is being installed.
var ErrUpdateBusy = errors.New("an update is already being installed")

// updateState is what rbd knows about updates.
type updateState struct {
	installing bool
	// trying is true when this start is a trial of a new slot.
	trying    bool
	confirmed bool
	message   string
	// latest is the newest release found by the online check.
	latest latestRelease
}

// pendingUpdate is saved after installing, so the next start knows what
// happened.
type pendingUpdate struct {
	Version     string `json:"version"`
	FromVersion string `json:"fromVersion"`
}

// board returns this image's board name ("" if unknown).
func (a *Agent) board() string {
	data, _ := os.ReadFile(a.paths.BoardFile())
	return strings.TrimSpace(string(data))
}

// singleSystemBoard is the board of the Pi Zero 2 W, 3B and 3B+ image
// (image/build.sh). That image has one copy of the system, so it can't
// install update files.
const singleSystemBoard = "rpi3"

// singleSystem reports whether this image has one copy of the system (the
// Pi Zero 2 W/3B/3B+ image): new versions are flashed instead of installed.
func (a *Agent) singleSystem() bool { return a.board() == singleSystemBoard }

// trustedUpdateKeys reads the public keys built into the image.
func (a *Agent) trustedUpdateKeys() []ed25519.PublicKey {
	var keys []ed25519.PublicKey
	files, _ := filepath.Glob(filepath.Join(a.paths.UpdateKeysDir(), "*.pub"))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if key, err := update.ParsePublicKey(string(data)); err == nil {
			keys = append(keys, key)
		} else {
			slog.Warn("ignoring an unreadable update key", "file", file)
		}
	}
	return keys
}

// updateStatus describes updates for the status page.
func (a *Agent) updateStatus() api.UpdateStatus {
	s := api.UpdateStatus{
		Board:          a.board(),
		CanInstall:     !a.singleSystem() && len(a.trustedUpdateKeys()) > 0,
		SingleSystem:   a.singleSystem(),
		Installing:     a.update.installing,
		Message:        a.update.message,
		CanCheckOnline: version.Repository != "",
		CheckOnline:    a.cfg.Updates.CheckOnline,
	}
	if a.update.latest.TagName != "" && a.update.latest.TagName != version.Version {
		s.LatestVersion, s.LatestURL = a.update.latest.TagName, a.update.latest.HTMLURL
	}
	return s
}

// InstallUpdate installs an update file read from r (streamed from the
// browser through rbweb) and then restarts into it. It takes minutes; other
// requests keep working meanwhile.
func (a *Agent) InstallUpdate(r io.Reader) error {
	a.mu.Lock()
	if a.mode != api.ModeNormal {
		a.mu.Unlock()
		return ErrWrongMode
	}
	if a.update.installing {
		a.mu.Unlock()
		return ErrUpdateBusy
	}
	a.update.installing = true
	a.update.message = "Installing an update…"
	a.mu.Unlock()

	newVersion, err := a.writeUpdate(r)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.update.installing = false
	if err != nil {
		slog.Error("update failed", "error", err)
		a.update.message = "The update was not installed: " + err.Error()
		return &config.ValidationError{Field: "update", Message: a.update.message}
	}

	pending, _ := json.Marshal(pendingUpdate{Version: newVersion, FromVersion: version.Version})
	if err := config.WriteFileAtomic(a.paths.UpdatePendingFile(), pending, 0o644); err != nil {
		return err
	}
	slog.Warn("update installed; restarting to try it", "version", newVersion)
	a.update.message = fmt.Sprintf("Update %s installed. RasBeacon is restarting to try it (about 2 minutes).", newVersion)
	a.afterFunc(switchDelay, func() {
		// "0 tryboot": start the other slot once, on trial.
		_, _ = a.runner.Run("systemctl", "reboot", "0 tryboot")
	})
	return nil
}

// writeUpdate checks the update file and writes it into the other slot. It
// returns the new version.
func (a *Agent) writeUpdate(r io.Reader) (string, error) {
	board := a.board()
	if board == "" {
		return "", errors.New("this image doesn't know its board, so it can't check updates")
	}
	if a.singleSystem() {
		return "", errors.New("this image has one copy of the system, so new versions are flashed instead")
	}
	pkg, err := update.Open(r, a.trustedUpdateKeys(), board)
	if err != nil {
		return "", err
	}
	slog.Info("update file signature is valid; writing the other slot", "version", pkg.Manifest.Version)

	targets := map[string]update.Target{}
	for _, part := range []string{"boot", "system"} {
		device, err := os.OpenFile(a.paths.SlotDevice("other", part), os.O_WRONLY, 0)
		if err != nil {
			return "", fmt.Errorf("opening the other slot's %s partition: %w", part, err)
		}
		defer device.Close()
		size, err := device.Seek(0, io.SeekEnd)
		if err != nil {
			return "", err
		}
		targets[part] = update.Target{Device: device, Size: size}
	}

	bundle, wait, err := a.decompress(pkg)
	if err != nil {
		return "", err
	}
	writeErr := update.WriteBundle(bundle, targets)
	_, _ = io.Copy(io.Discard, bundle) // let the decompressor finish
	if err := wait(); err != nil && writeErr == nil {
		writeErr = fmt.Errorf("decompressing the update: %w", err)
	}
	if writeErr != nil {
		return "", writeErr
	}
	// Everything was written; now check it is exactly what was signed.
	if err := pkg.Verify(); err != nil {
		return "", err
	}
	for part, target := range targets {
		if err := target.Device.(*os.File).Sync(); err != nil {
			return "", fmt.Errorf("saving the %s partition: %w", part, err)
		}
	}
	return pkg.Manifest.Version, nil
}

// zstdDecompress runs the zstd program to decompress the bundle as it
// streams in. wait reports zstd's result once its output was read.
func zstdDecompress(compressed io.Reader) (io.Reader, func() error, error) {
	cmd := exec.Command("zstd", "--decompress", "--stdout")
	cmd.Stdin = compressed
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("starting zstd: %w", err)
	}
	return out, cmd.Wait, nil
}

// checkTrialStart runs at startup. If the bootloader started this slot on
// trial, it schedules the confirmation (and the fallback restart). If an
// update was pending but this is NOT a trial, the trial failed and the old
// slot is running again.
func (a *Agent) checkTrialStart() {
	flag, _ := os.ReadFile(a.paths.TrybootFlag())
	a.update.trying = strings.Trim(string(flag), "\x00") != ""

	var pending pendingUpdate
	hasPending := config.ReadJSON(a.paths.UpdatePendingFile(), &pending) == nil

	switch {
	case a.update.trying:
		slog.Warn("running a new system on trial", "version", version.Version)
		a.afterFunc(rollbackAfter, a.rollbackIfUnconfirmed)
	case hasPending:
		slog.Error("the updated system did not start correctly; running the previous one", "update", pending.Version)
		a.update.message = fmt.Sprintf("The update to %s didn't start correctly, so RasBeacon went back to %s.", pending.Version, version.Version)
		_ = os.Remove(a.paths.UpdatePendingFile())
	}
}

// scheduleConfirmation is called once the new slot started normally.
func (a *Agent) scheduleConfirmation() {
	if a.update.trying {
		a.afterFunc(confirmAfter, a.confirmUpdate)
	}
}

// confirmUpdate makes the slot on trial the default.
func (a *Agent) confirmUpdate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.update.confirmed || a.mode != api.ModeNormal {
		return
	}
	// rpi-slot-tryboot prints autoboot.txt for the slot that is running now.
	autoboot, err := a.runner.Run("rpi-slot-tryboot")
	if err != nil || !strings.Contains(autoboot, "boot_partition=") {
		slog.Error("could not confirm the update", "error", err)
		return
	}
	if err := config.WriteFileAtomic(a.paths.AutobootFile(), []byte(autoboot), 0o644); err != nil {
		slog.Error("could not confirm the update", "error", err)
		return
	}
	a.update.confirmed = true
	_ = os.Remove(a.paths.UpdatePendingFile())
	a.update.message = "Updated to " + version.Version + "."
	slog.Info("update confirmed: this system is now the default", "version", version.Version)
}

// rollbackIfUnconfirmed restarts into the previous slot if the trial
// didn't succeed in time.
func (a *Agent) rollbackIfUnconfirmed() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.update.confirmed {
		return
	}
	slog.Error("the new system was not confirmed in time; restarting into the previous one")
	_, _ = a.runner.Run("systemctl", "reboot")
}

// --- The optional online check ---

// updateCheckInterval is how often the online check runs (when on).
const updateCheckInterval = 6 * time.Hour

// latestRelease is the part of GitHub's answer RasBeacon reads.
type latestRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// RunUpdateCheck asks GitHub for the newest release every few hours, while
// the owner has the check turned on. cmd/rbd starts it on the device.
func (a *Agent) RunUpdateCheck() {
	if version.Repository == "" {
		return // this build doesn't know where its releases are
	}
	client := &http.Client{Timeout: 20 * time.Second}
	lastCheck := time.Time{}
	for {
		time.Sleep(time.Minute)
		a.mu.Lock()
		due := a.cfg.Updates.CheckOnline && a.uplink.status.Connected && time.Since(lastCheck) >= updateCheckInterval
		a.mu.Unlock()
		if !due {
			continue
		}
		lastCheck = time.Now()
		release, err := fetchLatestRelease(client, githubAPI, version.Repository)
		if err != nil {
			slog.Warn("update check failed", "error", err)
			continue
		}
		a.mu.Lock()
		a.update.latest = release
		a.mu.Unlock()
	}
}

// githubAPI is GitHub's public API.
const githubAPI = "https://api.github.com"

// fetchLatestRelease asks GitHub's API (at apiURL) for a repository's
// newest release.
func fetchLatestRelease(client *http.Client, apiURL, repository string) (latestRelease, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL+"/repos/"+repository+"/releases/latest", nil)
	if err != nil {
		return latestRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "RasBeacon/"+version.Version)
	resp, err := client.Do(req)
	if err != nil {
		return latestRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return latestRelease{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var release latestRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return latestRelease{}, err
	}
	return release, nil
}

// UpdateUpdateSettings saves the online check setting.
func (a *Agent) UpdateUpdateSettings(req api.UpdateSettings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	newCfg := a.cfg.Clone()
	newCfg.Updates.CheckOnline = req.CheckOnline
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg = newCfg
	if !req.CheckOnline {
		a.update.latest = latestRelease{}
	}
	return nil
}
