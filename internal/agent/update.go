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
//     means the next start is the old slot again.) If that restart fails,
//     the System page says so and offers "Restart to finish the update".
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
	"sort"
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
	// confirmRetry: how long to wait before looking again when the web UI
	// or the hotspot isn't running yet at confirmAfter. Until rollbackAfter
	// at most.
	confirmRetry = 15 * time.Second
)

// ErrUpdateBusy means another update is being installed.
var ErrUpdateBusy = errors.New("an update is already being installed")

// updateState is what rbd knows about updates.
type updateState struct {
	installing bool
	// trying is true when this start is a trial of a new slot.
	trying    bool
	confirmed bool
	// confirmChecks counts how often confirmUpdate looked, so it stops
	// looking again before rollbackAfter.
	confirmChecks int
	message       string
	// latest is the release the online check offers (the newest one this
	// RasBeacon can install, see fetchUpdateRelease), newest the newest
	// release of all, lastCheck when it last asked, and checkError why that
	// failed.
	latest     latestRelease
	newest     string
	lastCheck  time.Time
	checkError string
	// refused is set when an update file was refused because it needs a
	// newer RasBeacon to install it (update.NeedsReflashError).
	refused *api.UpdateReflash
	// restartPending is true when an update is installed but RasBeacon
	// hasn't restarted into it yet: the restart failed, or rbd or the
	// power stopped first.
	restartPending bool
}

// pendingUpdate is saved after installing, so the next start knows what
// happened.
type pendingUpdate struct {
	Version     string `json:"version"`
	FromVersion string `json:"fromVersion"`
	// Restarting is set just before RasBeacon restarts into the update, so
	// the next start can tell a trial that failed (true) from one that
	// never began (false). Files from RasBeacon 0.1 don't have it.
	Restarting bool `json:"restarting,omitempty"`
}

// trybootCommand restarts the device into the other slot once, on trial:
// "0 tryboot" is the Raspberry Pi firmware's reboot argument for that.
// systemd 257 (Debian 13) refuses it as a plain extra word ("Too many
// arguments"), so it goes in --reboot-argument.
var trybootCommand = []string{"systemctl", "reboot", "--reboot-argument=0 tryboot"}

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
		RestartPending: a.update.restartPending,
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
		if !canInstallOver(latest, version.Version) {
			s.Reflash = &api.UpdateReflash{Version: latest.TagName, MinFromVersion: latest.minFromVersion, URL: latest.HTMLURL}
		} else if a.update.newest != latest.TagName {
			s.NewerVersion = a.update.newest
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
	a.afterFunc(switchDelay, func() { _ = a.restartIntoUpdate() })
	return nil
}

// RestartIntoUpdate restarts into an installed update that is waiting for
// its restart ("Restart to finish the update" on the System page). The
// restart happens shortly after the request is answered; if it fails, the
// status says why.
func (a *Agent) RestartIntoUpdate() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var pending pendingUpdate
	if a.update.trying || config.ReadJSON(a.paths.UpdatePendingFile(), &pending) != nil {
		return &config.ValidationError{Field: "update", Message: "No installed update is waiting for a restart."}
	}
	slog.Info("restart into the update requested from the web UI", "version", pending.Version)
	a.update.message = fmt.Sprintf("RasBeacon is restarting to try %s (about 2 minutes).", pending.Version)
	a.afterFunc(switchDelay, func() { _ = a.restartIntoUpdate() })
	return nil
}

// restartIntoUpdate restarts into the installed update, on trial. It first
// notes in the pending file that the restart was asked for. If the restart
// command fails, it takes the note back, logs the error and keeps the
// update waiting, so the System page can show why and offer the restart
// again.
func (a *Agent) restartIntoUpdate() error {
	a.mu.Lock()
	var pending pendingUpdate
	if err := config.ReadJSON(a.paths.UpdatePendingFile(), &pending); err != nil {
		a.mu.Unlock()
		return err
	}
	pending.Restarting = true
	err := a.savePendingUpdate(pending)
	a.mu.Unlock()
	if err == nil {
		// Not under the lock: the command may take a moment.
		_, err = a.runner.Run(trybootCommand[0], trybootCommand[1:]...)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		pending.Restarting = false
		_ = a.savePendingUpdate(pending)
		slog.Error("could not restart into the update", "version", pending.Version, "error", err)
		a.update.restartPending = true
		a.update.message = fmt.Sprintf("Update %s is installed, but RasBeacon couldn't restart into it: %v", pending.Version, err)
		return err
	}
	a.update.restartPending = false
	return nil
}

// savePendingUpdate writes the pending file. The caller holds a.mu.
func (a *Agent) savePendingUpdate(pending pendingUpdate) error {
	data, _ := json.Marshal(pending)
	return config.WriteFileAtomic(a.paths.UpdatePendingFile(), data, 0o644)
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
// update is pending but this is NOT a trial, either RasBeacon never
// restarted into it (it waits for "Restart to finish the update"), or the
// trial failed and the old slot is running again.
func (a *Agent) checkTrialStart() {
	flag, _ := os.ReadFile(a.paths.TrybootFlag())
	a.update.trying = strings.Trim(string(flag), "\x00") != ""

	var pending pendingUpdate
	hasPending := config.ReadJSON(a.paths.UpdatePendingFile(), &pending) == nil

	switch {
	case a.update.trying:
		slog.Warn("running a new system on trial", "version", version.Version)
		a.afterFunc(rollbackAfter, a.rollbackIfUnconfirmed)
	case hasPending && !pending.Restarting:
		// Installed, but RasBeacon never restarted into it: keep it waiting.
		slog.Warn("an installed update is waiting for a restart", "update", pending.Version)
		a.update.restartPending = true
		a.update.message = fmt.Sprintf("Update %s is installed. Restart RasBeacon to finish installing it.", pending.Version)
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
	// The owner must still have the settings page and the hotspot, or they
	// would have no way back to the old system. "systemctl is-active" fails
	// unless every unit named is running.
	if _, err := a.runner.Run("systemctl", "is-active", rbwebService, hostapdService); err != nil {
		a.update.confirmChecks++
		if wait := confirmAfter + time.Duration(a.update.confirmChecks)*confirmRetry; wait < rollbackAfter {
			slog.Warn("not confirming the update yet: the web UI or the hotspot isn't running", "error", err)
			a.afterFunc(confirmRetry, a.confirmUpdate)
		} else {
			slog.Error("not confirming the update: the web UI or the hotspot isn't running", "error", err)
		}
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
	release, newest, err := fetchUpdateRelease(updateHTTP, githubAPI, version.Repository, version.Version)
	a.mu.Lock()
	a.update.lastCheck = a.now()
	if err != nil {
		slog.Warn("update check failed", "error", err)
		a.update.checkError = "Couldn't ask GitHub: " + err.Error()
		a.mu.Unlock()
		return err
	}
	a.update.latest, a.update.newest, a.update.checkError = release, newest, ""
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
	} else if s.NewerVersion != "" {
		message += fmt.Sprintf(" Install it first: %s can only be installed over it.", s.NewerVersion)
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

// latestRelease is the part of GitHub's answer RasBeacon reads about one
// release.
type latestRelease struct {
	TagName    string         `json:"tag_name"`
	HTMLURL    string         `json:"html_url"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
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

// releasesToRead is how many of the newest releases the online check reads.
const releasesToRead = 30

// fetchUpdateRelease asks GitHub's API (at apiURL) for a repository's
// releases and picks the one to offer a RasBeacon that runs version
// running: the newest release that can be installed over it. A newer
// release may need a newer starting version (its .update.json says so, for
// example when the update signing key changed), so a device a few versions
// behind moves up one step at a time: after each update, the next check
// offers the next step.
//
// It returns the release to offer and the tag of the newest release. When
// no newer release can be installed over running, the newest one is
// returned, and the System page shows how to flash it. Drafts and
// pre-releases are never offered. With no newer release, the release is
// empty.
func fetchUpdateRelease(client *http.Client, apiURL, repository, running string) (latestRelease, string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d", apiURL, repository, releasesToRead)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return latestRelease{}, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "RasBeacon/"+version.Version)
	resp, err := client.Do(req)
	if err != nil {
		return latestRelease{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return latestRelease{}, "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var releases []latestRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&releases); err != nil {
		return latestRelease{}, "", err
	}

	// The published releases newer than running, newest first.
	var newer []latestRelease
	for _, release := range releases {
		result, ok := version.Compare(release.TagName, running)
		if ok && result > 0 && !release.Draft && !release.Prerelease {
			newer = append(newer, release)
		}
	}
	if len(newer) == 0 {
		return latestRelease{}, "", nil
	}
	sort.SliceStable(newer, func(i, j int) bool {
		result, _ := version.Compare(newer[i].TagName, newer[j].TagName)
		return result > 0
	})

	// The first (newest) one that can be installed over running.
	for i := range newer {
		newer[i].minFromVersion = readMinFromVersion(client, newer[i])
		if canInstallOver(newer[i], running) {
			return newer[i], newer[0].TagName, nil
		}
	}
	return newer[0], newer[0].TagName, nil
}

// readMinFromVersion reads the oldest version that can install a release's
// update from its .update.json file. Without the file (a release made
// before it existed) or when it can't be read, it is "": RasBeacon then
// finds out when the update file is installed.
func readMinFromVersion(client *http.Client, release latestRelease) string {
	for _, asset := range release.Assets {
		if !strings.HasSuffix(asset.Name, ".update.json") {
			continue
		}
		info, err := fetchUpdateInfo(client, asset.URL)
		if err != nil {
			slog.Warn("could not read the release's update info", "release", release.TagName, "error", err)
			return ""
		}
		if info.Version != release.TagName {
			return ""
		}
		return info.MinFromVersion
	}
	return ""
}

// canInstallOver reports whether a release's update can be installed over
// version running, as far as its .update.json says.
func canInstallOver(release latestRelease, running string) bool {
	if release.minFromVersion == "" {
		return true
	}
	result, ok := version.Compare(running, release.minFromVersion)
	return !ok || result >= 0
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
