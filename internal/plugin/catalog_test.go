package plugin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The catalog client against a fake GitHub: what a source's newest release
// lists, GitHub's refusals, and downloading a plugin.

// fakeGitHub answers requests by URL, like api.github.com and github.com
// would; anything else is 404. It records the request headers.
type fakeGitHub struct {
	files   map[string]string
	answers map[string]func(w http.ResponseWriter)
	headers []http.Header
}

func (f *fakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	f.headers = append(f.headers, req.Header.Clone())
	rec := httptest.NewRecorder()
	url := req.URL.String()
	if answer, ok := f.answers[url]; ok {
		answer(rec)
	} else if body, ok := f.files[url]; ok {
		_, _ = io.WriteString(rec, body)
	} else {
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

func (f *fakeGitHub) client() *http.Client { return &http.Client{Transport: f} }

const releaseURL = "https://api.github.com/repos/example/plugins/releases/latest"

func asset(name string, size int) string {
	return `{"name": "` + name + `", "size": ` + strconv.Itoa(size) + `, "browser_download_url": "https://github.com/example/plugins/releases/download/v1/` + name + `"}`
}

func TestFetchSource_DescribedAndUndescribedPlugins(t *testing.T) {
	gh := &fakeGitHub{files: map[string]string{
		releaseURL: `{"tag_name": "v1", "assets": [` + strings.Join([]string{
			asset("notes.rba", 2000), asset("extra.rba", 3000), asset("notes.png", 4), asset(CatalogFile, 100), asset("README.md", 10),
		}, ",") + `]}`,
		"https://github.com/example/plugins/releases/download/v1/" + CatalogFile: `{"plugins": [
			{"file": "notes.rba", "id": "notes", "name": "Trip Notes", "version": "1.2", "icon": "notes.png", "downloadSize": 5000},
			{"file": "missing.rba", "name": "Not in the release"}
		]}`,
		"https://github.com/example/plugins/releases/download/v1/notes.png": "\x89PNG",
	}}

	entries, err := FetchSource(gh.client(), "example/plugins")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want notes and extra", entries)
	}
	notes, extra := entries[0], entries[1]
	if notes.Name != "Trip Notes" || notes.Version != "1.2" || notes.Size != 2000 || notes.DownloadSize != 5000 {
		t.Errorf("notes = %+v", notes)
	}
	if notes.Icon != "data:image/png;base64,iVBORw==" {
		t.Errorf("icon = %q", notes.Icon)
	}
	if !strings.HasSuffix(notes.URL(), "/notes.rba") {
		t.Errorf("URL = %q", notes.URL())
	}
	// Not in the catalog file: listed by its file name.
	if extra.Name != "extra" || extra.Size != 3000 || extra.Version != "" {
		t.Errorf("extra = %+v", extra)
	}
	// As GitHub's documentation asks.
	if h := gh.headers[0]; h.Get("Accept") != "application/vnd.github+json" || h.Get("User-Agent") != "RasBeacon" {
		t.Errorf("API headers = %v", h)
	}
}

func TestFetchSource_Refusals(t *testing.T) {
	reset := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	tests := []struct {
		name   string
		answer func(w http.ResponseWriter)
		check  func(t *testing.T, err error)
	}{
		{
			name:   "no releases",
			answer: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) },
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "has no releases") {
					t.Errorf("err = %v", err)
				}
			},
		},
		{
			name: "out of questions for the hour",
			answer: func(w http.ResponseWriter) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
				w.WriteHeader(http.StatusForbidden)
			},
			check: func(t *testing.T, err error) {
				var limited *RateLimited
				if !errors.As(err, &limited) || !limited.Until.Equal(reset) {
					t.Errorf("err = %v, want RateLimited until %v", err, reset)
				}
				if !strings.Contains(err.Error(), "GitHub asks to wait") {
					t.Errorf("message = %q", err)
				}
			},
		},
		{
			name: "too many requests, with Retry-After",
			answer: func(w http.ResponseWriter) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
			},
			check: func(t *testing.T, err error) {
				var limited *RateLimited
				if !errors.As(err, &limited) || time.Until(limited.Until) > 2*time.Minute {
					t.Errorf("err = %v, want RateLimited for about a minute", err)
				}
			},
		},
		{
			name:   "another refusal",
			answer: func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "GitHub answered 502") {
					t.Errorf("err = %v", err)
				}
			},
		},
		{
			name:   "an answer that isn't JSON",
			answer: func(w http.ResponseWriter) { _, _ = io.WriteString(w, "<html>") },
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "can't be read") {
					t.Errorf("err = %v", err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{answers: map[string]func(w http.ResponseWriter){releaseURL: tc.answer}}
			_, err := FetchSource(gh.client(), "example/plugins")
			tc.check(t, err)
		})
	}
}

func TestFetchSource_ABrokenCatalogFileIsAnError(t *testing.T) {
	gh := &fakeGitHub{files: map[string]string{
		releaseURL: `{"assets": [` + asset(CatalogFile, 10) + `]}`,
		"https://github.com/example/plugins/releases/download/v1/" + CatalogFile: `{not json`,
	}}
	if _, err := FetchSource(gh.client(), "example/plugins"); err == nil || !strings.Contains(err.Error(), CatalogFile) {
		t.Errorf("err = %v, want one naming %s", err, CatalogFile)
	}
}

func TestDownload(t *testing.T) {
	url := "https://github.com/example/plugins/releases/download/v1/notes.rba"
	gh := &fakeGitHub{files: map[string]string{url: "RBA-BYTES"}}
	dst := filepath.Join(t.TempDir(), "notes.rba")

	if err := Download(gh.client(), url, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "RBA-BYTES" {
		t.Errorf("downloaded %q", got)
	}
	if err := Download(gh.client(), "https://example.com/notes.rba", dst); err == nil {
		t.Error("only GitHub releases may be downloaded")
	}
	if err := Download(gh.client(), url+".missing", dst); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a missing file = %v", err)
	}
}
