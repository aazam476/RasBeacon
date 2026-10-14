package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// manifestTypes are the manifest kinds RasBeacon understands: Docker's and
// OCI's, single images and lists of images for several systems.
var manifestTypes = []string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}

// registryManifest covers both an image list and a single image.
type registryManifest struct {
	MediaType string `json:"mediaType"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
	Config struct {
		Size int64 `json:"size"`
	} `json:"config"`
	Layers []struct {
		Size int64 `json:"size"`
	} `json:"layers"`
}

// SplitRef splits an image name into the registry's host and the
// repository: docker.io/adguard/adguardhome:v1 → registry-1.docker.io,
// adguard/adguardhome.
func SplitRef(ref string) (host, repository string) {
	name := ref
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name = name[:i] // the tag
	}
	host, repository, _ = strings.Cut(name, "/")
	if host == "docker.io" {
		host = "registry-1.docker.io"
		if !strings.Contains(repository, "/") {
			repository = "library/" + repository
		}
	}
	return host, repository
}

// DownloadSize asks the image's registry how much an image download is for
// arm64: the sum of its compressed layers. client decides the way out
// (through the VPN).
func DownloadSize(client *http.Client, image RegistryImage) (int64, error) {
	host, repository := SplitRef(image.Ref)
	r := registry{client: client, host: host, repository: repository}
	m, err := r.manifest(image.Digest)
	if err != nil {
		return 0, err
	}
	if len(m.Manifests) > 0 {
		digest := ""
		for _, entry := range m.Manifests {
			if entry.Platform.OS == "linux" && entry.Platform.Architecture == "arm64" {
				digest = entry.Digest
				break
			}
		}
		if digest == "" {
			return 0, errors.New("the image has no version for the Raspberry Pi (linux/arm64)")
		}
		if m, err = r.manifest(digest); err != nil {
			return 0, err
		}
	}
	size := m.Config.Size
	for _, layer := range m.Layers {
		size += layer.Size
	}
	return size, nil
}

type registry struct {
	client     *http.Client
	host       string
	repository string
	token      string
}

func (r *registry) manifest(digest string) (*registryManifest, error) {
	u := "https://" + r.host + "/v2/" + r.repository + "/manifests/" + digest
	resp, err := r.get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && r.token == "" {
		if err := r.login(resp.Header.Get("WWW-Authenticate")); err != nil {
			return nil, err
		}
		resp.Body.Close()
		if resp, err = r.get(u); err != nil {
			return nil, err
		}
		defer resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the image registry answered %s", resp.Status)
	}
	var m registryManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("the image registry's answer can't be read: %w", err)
	}
	return &m, nil
}

func (r *registry) get(u string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", strings.Join(manifestTypes, ", "))
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	return r.client.Do(req)
}

// login gets an anonymous token, as a registry's 401 answer asks:
// Bearer realm="…",service="…",scope="…".
func (r *registry) login(challenge string) error {
	if !strings.HasPrefix(challenge, "Bearer ") {
		return errors.New("the image registry wants a login RasBeacon doesn't have")
	}
	params := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(challenge, "Bearer "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			params[key] = strings.Trim(value, `"`)
		}
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Scheme != "https" {
		return errors.New("the image registry's login address can't be used")
	}
	q := realm.Query()
	if params["service"] != "" {
		q.Set("service", params["service"])
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + r.repository + ":pull"
	}
	q.Set("scope", scope)
	realm.RawQuery = q.Encode()
	resp, err := r.client.Get(realm.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the image registry's login answered %s", resp.Status)
	}
	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		return err
	}
	r.token = token.Token
	if r.token == "" {
		r.token = token.AccessToken
	}
	if r.token == "" {
		return errors.New("the image registry gave no login token")
	}
	return nil
}
