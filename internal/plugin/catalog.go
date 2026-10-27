package plugin

// This file reads the catalog: for each source (a GitHub repository), its
// newest release's list of files, through GitHub's API
// (api.github.com/repos/OWNER/REPO/releases/latest). No GitHub account is
// needed; GitHub allows 60 such questions an hour from one address, which
// is why RasBeacon only asks when the owner opens the catalog, and once a
// day for updates.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A source is a GitHub repository ("owner/repo") whose newest release
// holds .rba files. The release may also hold CatalogFile, which describes
// them (name, version, description, icon), so the catalog can show them
// without downloading every plugin.
const (
	// BuiltInSource is RasBeacon's own plugins.
	BuiltInSource = "aazam476/RasBeacon-plugins"
	CatalogFile   = "rasbeacon-catalog.json"
	githubAPI     = "https://api.github.com"
	maxIconBytes  = 128 << 10
)

var sourcePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// CheckSource checks an "owner/repo" name, also accepting a GitHub address.
func CheckSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "https://")
	source = strings.TrimPrefix(source, "github.com/")
	source = strings.TrimSuffix(strings.TrimSuffix(source, "/"), ".git")
	if !sourcePattern.MatchString(source) || strings.Contains(source, "..") {
		return "", errors.New("enter a GitHub repository like owner/repo")
	}
	return source, nil
}

// Entry is one plugin a source offers.
type Entry struct {
	Source string `json:"source"`
	// File is the .rba's name in the release.
	File        string   `json:"file"`
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Boards      []string `json:"boards,omitempty"`
	// MinRasBeacon is the oldest RasBeacon it works with ("" = any).
	MinRasBeacon string `json:"minRasBeacon,omitempty"`
	// Size is the .rba's size; DownloadSize adds the image download when
	// the image comes from a registry.
	Size         int64 `json:"size"`
	DownloadSize int64 `json:"downloadSize,omitempty"`
	// Icon is a data: URL, if the release has one.
	Icon string `json:"icon,omitempty"`

	url string
}

// URL is where the .rba is downloaded from.
func (e Entry) URL() string { return e.url }

// catalogEntry is one plugin in CatalogFile.
type catalogEntry struct {
	File         string   `json:"file"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Permissions  []string `json:"permissions"`
	Boards       []string `json:"boards"`
	MinRasBeacon string   `json:"minRasBeacon"`
	DownloadSize int64    `json:"downloadSize"`
	// IconFile is a .png in the same release.
	IconFile string `json:"icon"`
}

// Catalog is CatalogFile.
type Catalog struct {
	Plugins []catalogEntry `json:"plugins"`
}

// RateLimited is GitHub refusing more questions for a while.
type RateLimited struct{ Until time.Time }

// Error says until when GitHub asks to wait.
func (e *RateLimited) Error() string {
	return "GitHub asks to wait before checking again (until " + e.Until.Local().Format("15:04") + ")"
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// FetchSource lists the plugins in a source's newest release. client
// decides the way out (through the VPN).
func FetchSource(client *http.Client, source string) ([]Entry, error) {
	resp, err := githubGet(client, githubAPI+"/repos/"+source+"/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s has no releases (or doesn't exist)", source)
	}
	if err := githubError(resp); err != nil {
		return nil, err
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("GitHub's answer can't be read: %w", err)
	}
	assets := map[string]int{}
	for i, a := range release.Assets {
		assets[a.Name] = i
	}

	var entries []Entry
	described := map[string]bool{}
	if i, ok := assets[CatalogFile]; ok {
		var catalog Catalog
		if err := getJSON(client, release.Assets[i].URL, &catalog); err != nil {
			return nil, fmt.Errorf("%s in %s: %w", CatalogFile, source, err)
		}
		for _, c := range catalog.Plugins {
			j, ok := assets[c.File]
			if !ok || !strings.HasSuffix(c.File, ".rba") {
				continue
			}
			e := Entry{
				Source: source, File: c.File, ID: c.ID, Name: c.Name, Version: c.Version,
				Description: c.Description, Permissions: c.Permissions, Boards: c.Boards, MinRasBeacon: c.MinRasBeacon,
				Size: release.Assets[j].Size, DownloadSize: c.DownloadSize, url: release.Assets[j].URL,
			}
			if k, ok := assets[c.IconFile]; ok && strings.HasSuffix(c.IconFile, ".png") && release.Assets[k].Size <= maxIconBytes {
				if icon, err := getBytes(client, release.Assets[k].URL, maxIconBytes); err == nil {
					e.Icon = "data:image/png;base64," + base64.StdEncoding.EncodeToString(icon)
				}
			}
			if e.Name == "" {
				e.Name = strings.TrimSuffix(c.File, ".rba")
			}
			described[c.File] = true
			entries = append(entries, e)
		}
	}
	// .rba files the catalog file doesn't describe are listed by name.
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, ".rba") && !described[a.Name] {
			entries = append(entries, Entry{Source: source, File: a.Name, Name: strings.TrimSuffix(a.Name, ".rba"), Size: a.Size, url: a.URL})
		}
	}
	return entries, nil
}

// Download saves a release's .rba to dst.
func Download(client *http.Client, url, dst string) error {
	if !strings.HasPrefix(url, "https://github.com/") {
		return errors.New("plugins are only downloaded from GitHub releases")
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, MaxArchiveSize+1))
	if err == nil && n > MaxArchiveSize {
		err = errors.New("the plugin file is larger than 2 GB")
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// githubGet asks GitHub's API, as its documentation asks clients to.
func githubGet(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "RasBeacon")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return client.Do(req)
}

// githubError turns GitHub's refusals into errors; RateLimited when it
// asks to wait.
func githubError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		until := time.Now().Add(time.Hour)
		if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			until = time.Unix(reset, 0)
		} else if after, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			until = time.Now().Add(time.Duration(after) * time.Second)
		}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.StatusCode == http.StatusTooManyRequests {
			return &RateLimited{Until: until}
		}
	}
	return fmt.Errorf("GitHub answered %s", resp.Status)
}

// getJSON downloads a release's JSON file (at most 1 MB) into v.
func getJSON(client *http.Client, url string, v any) error {
	data, err := getBytes(client, url, 1<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// getBytes downloads a release's file of at most limit bytes.
func getBytes(client *http.Client, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://github.com/") {
		return nil, errors.New("not a GitHub release file")
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RasBeacon")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := githubError(resp); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("too large")
	}
	return data, nil
}

// NewerVersion reports whether version a is newer than b, comparing the
// numbers in them in order ("1.10.2" > "1.9"); other text is compared as
// text. When one is the other with more parts, more numbers are a later
// version ("1.0.1" > "1.0", a packaging revision "1.0-2" > "1.0"), but a
// word starts a pre-release, which comes before the release ("1.0-beta.1"
// < "1.0").
func NewerVersion(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		switch {
		case ea == nil && eb == nil && na != nb:
			return na > nb
		case (ea != nil || eb != nil) && pa[i] != pb[i]:
			return pa[i] > pb[i]
		}
	}
	switch {
	case len(pa) > len(pb):
		return isNumber(pa[len(pb)])
	case len(pb) > len(pa):
		return !isNumber(pb[len(pa)])
	}
	return false
}

// isNumber reports whether a version part is a number.
func isNumber(part string) bool {
	_, err := strconv.Atoi(part)
	return err == nil
}

// versionParts cuts "v1.10.2-1" into "1", "10", "2", "1".
func versionParts(v string) []string {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	return strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' || r == '+' || r == '_' })
}
