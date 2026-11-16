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
	"path"
	"strings"
)

// An image file (image.tar in a plugin, or a backup's copy) is a tar made
// by `docker save` or `podman save`. Besides the image itself it lists the
// names the image should get, and `podman load` gives it every one of them,
// taking a name away from any other image that had it. So before loading,
// RasBeacon reads those names: a plugin must not bring a name that belongs
// to another plugin's image, or that plugin would start this one's program.
//
// RasBeacon and Podman must read the same names, or a file could show
// RasBeacon harmless ones and Podman others. Podman finds the two files
// that hold them (manifest.json and index.json) more loosely than by their
// plain name:
//
//   - In a docker archive it takes the first entry whose cleaned path is
//     manifest.json ("./manifest.json", "././manifest.json" and
//     "x/../manifest.json" all are), and follows a link in its place.
//   - An OCI archive is unpacked into a folder first, where a later entry
//     replaces an earlier one of the same path, and an entry below a link
//     to a folder is written where the link points.
//
// So ImageNames finds the files the same loose way, and refuses a tar in
// which they could be read two ways: one of them twice, one that isn't a
// plain file, or any entry inside a link.

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

// maxImageEntries is the most entries an image file may have: a file for
// each layer and a few more, far fewer than this. The entries' paths are
// kept to check them against each other, so their number has a limit.
const maxImageEntries = 100_000

// entryPath is a tar entry's path as it ends up when the tar is unpacked
// into a folder: from the folder's top, without ".", ".." or doubled and
// trailing slashes ("/manifest.json" for "./x/../manifest.json").
func entryPath(name string) string { return path.Clean("/" + name) }

// ImageNames returns the names written in an image file. It fails if the
// file isn't an uncompressed image tar, or if its names could be read in
// more than one way (see above), because then nobody can tell which names
// podman would apply.
func ImageNames(file string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var names, entries []string
	// links are the entries that are links (to a file or a folder), and
	// seen the name files already read.
	links, seen := map[string]bool{}, map[string]bool{}
	r := tar.NewReader(f)
	for {
		header, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("the image isn't an uncompressed tar file")
		}
		entry := entryPath(header.Name)
		if len(entries) >= maxImageEntries {
			return nil, fmt.Errorf("the image has more than %d files", maxImageEntries)
		}
		entries = append(entries, entry)
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			links[entry] = true
		}
		if entry != "/manifest.json" && entry != "/index.json" {
			continue
		}
		// Named in the errors as the files are known ("manifest.json").
		name := strings.TrimPrefix(entry, "/")
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("the image's %s is not a plain file", name)
		}
		if seen[entry] {
			return nil, fmt.Errorf("the image has %s twice", name)
		}
		seen[entry] = true
		if entry == "/manifest.json" {
			var images []dockerManifest
			if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&images); err != nil {
				return nil, fmt.Errorf("the image's manifest.json can't be read: %w", err)
			}
			for _, image := range images {
				names = append(names, image.RepoTags...)
			}
			continue
		}
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
	}
	if len(seen) == 0 {
		return nil, errors.New("the image has no manifest.json or index.json")
	}
	// An entry inside a link would be written where the link points when
	// the tar is unpacked: over manifest.json or index.json, for example.
	if len(links) > 0 {
		for _, entry := range entries {
			for dir := path.Dir(entry); dir != "/"; dir = path.Dir(dir) {
				if links[dir] {
					return nil, fmt.Errorf("the image has a file inside a link (%s)", strings.TrimPrefix(entry, "/"))
				}
			}
		}
	}
	return names, nil
}

// InNameSpace reports whether an image name is in the plugins' name space:
// RasBeacon's own names for plugins' images (see LocalImage), in any
// spelling.
func InNameSpace(name string) bool {
	return strings.Contains(strings.ToLower(name), imageNameSpace)
}

// CheckImageNames refuses names that would take over another plugin's
// image: any plugin image name other than own (the name RasBeacon gives
// this plugin's image).
func CheckImageNames(names []string, own string) error {
	for _, name := range names {
		if name != own && InNameSpace(name) {
			return fmt.Errorf("its image carries another plugin's name, %s", name)
		}
	}
	return nil
}
