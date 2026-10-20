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
	"context"
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
	"slices"
	"strings"
	"time"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
	"rasbeacon/internal/notify"
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
	// latest is the newest release found by the online check, lastCheck
	// when it last asked, and checkError why that failed.
	latest     latestRelease
	lastCheck  time.Time
	checkError string
	// refused is set when an update file was refused because it needs a
	// newer RasBeacon to install it (update.NeedsReflashError).
	refused *api.UpdateReflash
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
		CheckHours:     a.cfg.Updates.CheckHours,
		CheckError:     a.update.checkError,
	}
	if !a.update.lastCheck.IsZero() {
		s.LastChecked = a.update.lastCheck.UTC().Format(time.RFC3339)
	}
	latest := a.update.latest
	if result, ok := version.Compare(latest.TagName, version.Version); ok && result > 0 {
		s.LatestVersion, s.LatestURL = latest.TagName, latest.HTMLURL
		if min := latest.minFromVersion; min != "" {
			if result, ok := version.Compare(version.Version, min); ok && result < 0 {
				s.Reflash = &api.UpdateReflash{Version: latest.TagName, MinFromVersion: min, URL: latest.HTMLURL}
			}
		}
	}
	// A refused update file says the same more surely (it was signed).
	if a.update.refused != nil {
		s.Reflash = a.update.refused
	}
	return s
}

// releaseURL is the release page of a version, when this build knows its
// repository.
func releaseURL(tag string) string {
	if version.Repository == "" {
		return ""
	}
	return "https://github.com/" + version.Repository + "/releases/tag/" + tag
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
		var needs *update.NeedsReflashError
		if errors.As(err, &needs) {
			a.update.refused = &api.UpdateReflash{Version: needs.Version, MinFromVersion: needs.MinFromVersion, URL: releaseURL(needs.Version)}
		}
		a.update.message = "The update was not installed: " + err.Error()
		return &config.ValidationError{Field: "update", Message: a.update.message}
	}

	a.update.refused = nil
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
	// No older versions, and none that need a newer RasBeacon to install.
	if err := update.CheckVersion(pkg.Manifest, version.Version); err != nil {
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

// updateHTTP asks GitHub and sends notifications.
var updateHTTP = &http.Client{Timeout: 20 * time.Second}

// RunUpdateCheck asks GitHub for the newest release as often as the owner
// chose (never, unless they chose a schedule). cmd/rbd starts it.
func (a *Agent) RunUpdateCheck() {
	if version.Repository == "" {
		return // this build doesn't know where its releases are
	}
	for {
		time.Sleep(time.Minute)
		if a.updateCheckDue() {
			_ = a.checkForUpdate()
		}
	}
}

// updateCheckDue reports whether it is time to ask GitHub: the owner chose
// a schedule, RasBeacon is online, and the last check is that long ago.
func (a *Agent) updateCheckDue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	every := time.Duration(a.cfg.Updates.CheckHours) * time.Hour
	return every > 0 && a.uplink.status.Connected && a.now().Sub(a.update.lastCheck) >= every
}

// CheckForUpdateNow asks GitHub right away ("Check now").
func (a *Agent) CheckForUpdateNow() (api.UpdateStatus, error) {
	if version.Repository == "" {
		return api.UpdateStatus{}, &config.ValidationError{Field: "update", Message: "This build doesn't know where its releases are."}
	}
	err := a.checkForUpdate()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		return api.UpdateStatus{}, &config.ValidationError{Field: "update", Message: a.update.checkError}
	}
	return a.updateStatus(), nil
}

// checkForUpdate asks GitHub for the newest release and, if it is new and
// a notification address is saved, announces it once.
func (a *Agent) checkForUpdate() error {
	release, err := fetchLatestRelease(updateHTTP, githubAPI, version.Repository)
	a.mu.Lock()
	a.update.lastCheck = a.now()
	if err != nil {
		slog.Warn("update check failed", "error", err)
		a.update.checkError = "Couldn't ask GitHub: " + err.Error()
		a.mu.Unlock()
		return err
	}
	a.update.latest, a.update.checkError = release, ""
	notifyURL := a.secrets.UpdateNotifyURL
	newer, ok := version.Compare(release.TagName, version.Version)
	announce := notifyURL != "" && ok && newer > 0 && release.TagName != a.cfg.Updates.NotifiedVersion
	a.mu.Unlock()
	if !announce {
		return nil
	}

	message := fmt.Sprintf("RasBeacon %s is available (this one runs %s).", release.TagName, version.Version)
	if s := a.updateStatusLocked(); s.Reflash != nil {
		message += " It needs a fresh install: open the System page for the steps."
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := notify.Send(ctx, updateHTTP, notifyURL, "RasBeacon "+release.TagName, message, release.HTMLURL); err != nil {
		slog.Warn("could not send the update notification", "error", err)
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	newCfg := a.cfg.Clone()
	newCfg.Updates.NotifiedVersion = release.TagName
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err == nil {
		a.cfg = newCfg
	}
	slog.Info("new version announced", "version", release.TagName)
	return nil
}

// updateStatusLocked is updateStatus, taking a.mu itself.
func (a *Agent) updateStatusLocked() api.UpdateStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.updateStatus()
}

// UpdateSettings returns the update check's schedule and notification
// address.
func (a *Agent) UpdateSettings() api.UpdateSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.UpdateSettings{CheckHours: a.cfg.Updates.CheckHours, NotifyURL: a.secrets.UpdateNotifyURL}
}

// UpdateUpdateSettings saves the schedule and the notification address.
func (a *Agent) UpdateUpdateSettings(req api.UpdateSettings) error {
	if !slices.Contains(config.UpdateCheckHours, req.CheckHours) {
		return &config.ValidationError{Field: "checkHours", Message: "Choose how often from the list."}
	}
	req.NotifyURL = strings.TrimSpace(req.NotifyURL)
	if req.NotifyURL != "" {
		if err := notify.CheckURL(req.NotifyURL); err != nil {
			return &config.ValidationError{Field: "notifyUrl", Message: err.Error()}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	newCfg := a.cfg.Clone()
	newCfg.Updates.CheckHours = req.CheckHours
	newSecrets := a.secrets.Clone()
	newSecrets.UpdateNotifyURL = req.NotifyURL
	if err := config.WriteJSON(a.paths.SecretsFile(), newSecrets, 0o600); err != nil {
		return err
	}
	if err := config.WriteJSON(a.paths.ConfigFile(), newCfg, 0o644); err != nil {
		return err
	}
	a.cfg, a.secrets = newCfg, newSecrets
	return nil
}

// SendTestNotification sends a test message to address, or to the saved
// one when address is empty.
func (a *Agent) SendTestNotification(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		a.mu.Lock()
		address = a.secrets.UpdateNotifyURL
		a.mu.Unlock()
	}
	if address == "" {
		return &config.ValidationError{Field: "notifyUrl", Message: "Enter the address first."}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := notify.Send(ctx, updateHTTP, address, "RasBeacon", "This is a test: RasBeacon will tell you here when a new version is out.", "")
	if err != nil {
		return &config.ValidationError{Field: "notifyUrl", Message: "The test didn't arrive: " + err.Error()}
	}
	return nil
}

// latestRelease is the part of GitHub's answer RasBeacon reads.
type latestRelease struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
	// minFromVersion comes from the release's .update.json file (see
	// update.Info): the oldest version that can install its update.
	minFromVersion string
}

// releaseAsset is one file of a release.
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// githubAPI is GitHub's public API.
var githubAPI = "https://api.github.com"

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
	// The release's update info says whether this RasBeacon can install it.
	// Without it (a release made before it existed), RasBeacon finds out
	// when the update file is installed.
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".update.json") {
			info, err := fetchUpdateInfo(client, asset.URL)
			if err != nil {
				slog.Warn("could not read the release's update info", "error", err)
			} else if info.Version == release.TagName {
				release.minFromVersion = info.MinFromVersion
			}
			break
		}
	}
	return release, nil
}

// fetchUpdateInfo downloads a release's small .update.json file.
func fetchUpdateInfo(client *http.Client, url string) (update.Info, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return update.Info{}, err
	}
	req.Header.Set("User-Agent", "RasBeacon/"+version.Version)
	resp, err := client.Do(req)
	if err != nil {
		return update.Info{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return update.Info{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var info update.Info
	err = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&info)
	return info, err
}
