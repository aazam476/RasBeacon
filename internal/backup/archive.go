package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// InfoFile describes the backup, first in the archive.
const InfoFile = "backup.json"

// Info is backup.json.
type Info struct {
	Created time.Time `json:"created"`
	// Version is the RasBeacon version that made it.
	Version string `json:"version"`
	Board   string `json:"board"`
	// Plugins is true when the plugins (files, images, data) are in it.
	Plugins bool `json:"plugins"`
}

// Writer writes a backup: an encrypted, gzipped tar archive.
type Writer struct {
	enc io.WriteCloser
	gz  *gzip.Writer
	tw  *tar.Writer
}

// Create starts a backup on w, encrypted with password, with info first.
func Create(w io.Writer, password string, info Info) (*Writer, error) {
	enc, err := NewWriter(w, password)
	if err != nil {
		return nil, err
	}
	gz, _ := gzip.NewWriterLevel(enc, gzip.BestSpeed)
	bw := &Writer{enc: enc, gz: gz, tw: tar.NewWriter(gz)}
	data, _ := json.MarshalIndent(info, "", "  ")
	if err := bw.AddBytes(InfoFile, data, 0o600); err != nil {
		return nil, err
	}
	return bw, nil
}

// AddBytes adds a file with the given contents.
func (bw *Writer) AddBytes(name string, data []byte, mode int64) error {
	if err := bw.tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := bw.tw.Write(data)
	return err
}

// AddFile adds a file from disk as name. A missing file is skipped.
func (bw *Writer) AddFile(name, file string) error {
	f, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := bw.tw.WriteHeader(&tar.Header{Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.Copy(bw.tw, f)
	return err
}

// AddDir adds the regular files under dir as name/..., skipping those skip
// says (by path relative to dir). A missing folder is skipped.
func (bw *Writer) AddDir(name, dir string, skip func(relative string) bool) error {
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(dir, p)
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if skip != nil && skip(relative) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // folders are made on the way; nothing else is kept
		}
		return bw.AddFile(name+"/"+relative, p)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Close finishes the archive and the encryption.
func (bw *Writer) Close() error {
	if err := bw.tw.Close(); err != nil {
		return err
	}
	if err := bw.gz.Close(); err != nil {
		return err
	}
	return bw.enc.Close()
}

// Extract decrypts a backup into dir (which must be empty) and returns its
// Info. Only regular files with clean relative names are written.
func Extract(r io.Reader, password, dir string) (Info, error) {
	plain, err := NewReader(r, password)
	if err != nil {
		return Info{}, err
	}
	gz, err := gzip.NewReader(plain)
	if err != nil {
		return Info{}, ErrWrongPassword
	}
	tr := tar.NewReader(gz)
	var info Info
	first := true
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, ErrWrongPassword) {
				return Info{}, err
			}
			return Info{}, fmt.Errorf("the backup file can't be read: %w", err)
		}
		name := path.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || path.IsAbs(name) || name == "." || strings.HasPrefix(name, "../") || name == ".." {
			return Info{}, fmt.Errorf("the backup file holds an unexpected entry %q", h.Name)
		}
		if first {
			first = false
			if name != InfoFile {
				return Info{}, ErrNotBackup
			}
			data, err := io.ReadAll(io.LimitReader(tr, 64<<10))
			if err != nil || json.Unmarshal(data, &info) != nil {
				return Info{}, ErrNotBackup
			}
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return Info{}, err
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(h.Mode).Perm()|0o600)
		if err != nil {
			return Info{}, err
		}
		_, err = io.Copy(f, tr)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			if errors.Is(err, ErrWrongPassword) {
				return Info{}, err
			}
			return Info{}, fmt.Errorf("the backup file can't be read: %w", err)
		}
	}
	if first {
		return Info{}, ErrNotBackup
	}
	return info, nil
}
