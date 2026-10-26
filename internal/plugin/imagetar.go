package plugin

// This file reads the image names inside a plugin's image.tar, so a plugin
// can't bring an image named like another plugin's.

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// An image file (image.tar in a plugin, or a backup's copy) is a tar made
// by `docker save` or `podman save`. Besides the image itself it lists the
// names the image should get, and `podman load` gives it every one of them,
// taking a name away from any other image that had it. So before loading,
// RasBeacon reads those names: a plugin must not bring a name that belongs
// to another plugin's image, or that plugin would start this one's program.

// imageNameSpace is the part of a name that marks a plugin's image (see
// LocalImage). Any name with it, in any spelling podman accepts
// ("localhost/rasbeacon-plugin/…", "rasbeacon-plugin/…"), is checked.
const imageNameSpace = "rasbeacon-plugin/"

// dockerManifest is one entry of manifest.json in a `docker save` tar.
type dockerManifest struct {
	RepoTags []string `json:"RepoTags"`
}

// ociIndex is index.json in an OCI tar (`podman save --format oci-archive`).
// An image's name is in one of two annotations.
type ociIndex struct {
	Manifests []struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"manifests"`
}

// ImageNames returns the names written in an image file. It fails if the
// file isn't an uncompressed image tar, because then nobody can tell which
// names podman would apply.
func ImageNames(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var names []string
	found := false
	r := tar.NewReader(f)
	for {
		header, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("the image isn't an uncompressed tar file")
		}
		switch strings.TrimPrefix(header.Name, "./") {
		case "manifest.json":
			var images []dockerManifest
			if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&images); err != nil {
				return nil, fmt.Errorf("the image's manifest.json can't be read: %w", err)
			}
			for _, image := range images {
				names = append(names, image.RepoTags...)
			}
			found = true
		case "index.json":
			var index ociIndex
			if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&index); err != nil {
				return nil, fmt.Errorf("the image's index.json can't be read: %w", err)
			}
			for _, m := range index.Manifests {
				for _, key := range []string{"org.opencontainers.image.ref.name", "io.containerd.image.name"} {
					if name := m.Annotations[key]; name != "" {
						names = append(names, name)
					}
				}
			}
			found = true
		}
	}
	if !found {
		return nil, errors.New("the image has no manifest.json or index.json")
	}
	return names, nil
}

// CheckImageNames refuses names that would take over another plugin's
// image: any plugin image name other than own (the name RasBeacon gives
// this plugin's image).
func CheckImageNames(names []string, own string) error {
	for _, name := range names {
		if name != own && strings.Contains(strings.ToLower(name), imageNameSpace) {
			return fmt.Errorf("its image carries another plugin's name, %s", name)
		}
	}
	return nil
}
