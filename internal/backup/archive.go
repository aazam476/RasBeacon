package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
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
	// Plugins is true when the plugins are in it. Backups made before
	// RasBeacon 0.2 held all of a plugin (files, image, data) or nothing.
	Plugins bool `json:"plugins"`
	// PluginContent says how much of each plugin is in it (see the
	// constants below); "" in backups made before RasBeacon 0.2.
	PluginContent string `json:"pluginContent,omitempty"`
}

// How much of the plugins a backup holds. Every one holds the plugins'
// settings and files; images left out are downloaded again on restore.
const (
	// PluginSettings: the plugins' settings, without their data.
	PluginSettings = "settings"
	// PluginData: also their data.
	PluginData = "data"
	// PluginEverything: also their images, so a restore works offline.
	PluginEverything = "everything"
)

// Writer writes a backup: a tar archive, compressed with zstd, then
// encrypted.
type Writer struct {
	enc  io.WriteCloser
	tw   *tar.Writer
	pipe *io.PipeWriter // into zstd
	zstd *exec.Cmd
	// zstdOut is zstd's error output, for the message when it fails.
	zstdOut bytes.Buffer
}

// Create starts a backup on w, encrypted with password, with info first.
// big says the backup may be large (plugins' data or images): it is then
// compressed a little less, so a Raspberry Pi Zero makes it in minutes and
// within its memory.
func Create(w io.Writer, password string, info Info, big bool) (*Writer, error) {
	enc, err := NewWriter(w, password)
	if err != nil {
		return nil, err
	}
	// zstd compresses better than gzip and much faster with every core
	// (-T0). Settings alone are a few kilobytes: they get the strongest
	// level.
	level := "-19"
	if big {
		level = "-9"
	}
	reader, pipe := io.Pipe()
	bw := &Writer{enc: enc, pipe: pipe}
	bw.zstd = exec.Command("zstd", "--quiet", "-T0", level, "--stdout")
	bw.zstd.Stdin, bw.zstd.Stdout, bw.zstd.Stderr = reader, enc, &bw.zstdOut
	if err := bw.zstd.Start(); err != nil {
		return nil, fmt.Errorf("starting zstd: %w", err)
	}
	bw.tw = tar.NewWriter(pipe)
	data, _ := json.MarshalIndent(info, "", "  ")
	if err := bw.AddBytes(InfoFile, data, 0o600); err != nil {
		bw.abort()
		return nil, err
	}
	return bw, nil
}

// abort stops zstd after a problem.
func (bw *Writer) abort() {
	_ = bw.pipe.CloseWithError(errors.New("backup stopped"))
	_ = bw.zstd.Wait()
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

// Close finishes the archive, the compression and the encryption.
func (bw *Writer) Close() error {
	if err := bw.tw.Close(); err != nil {
		bw.abort()
		return err
	}
	_ = bw.pipe.Close()
	if err := bw.zstd.Wait(); err != nil {
		return fmt.Errorf("zstd: %v %s", err, strings.TrimSpace(bw.zstdOut.String()))
	}
	return bw.enc.Close()
}

// decompress opens the archive inside a decrypted backup: zstd since
// RasBeacon 0.2, gzip before. stop ends zstd early (call it when done).
func decompress(plain io.Reader) (archive io.Reader, stop func(), err error) {
	br := bufio.NewReader(plain)
	magic, _ := br.Peek(4)
	switch {
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, nil, ErrWrongPassword
		}
		return gz, func() {}, nil
	case bytes.Equal(magic, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		cmd := exec.Command("zstd", "--quiet", "--decompress", "--stdout")
		cmd.Stdin = br
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, nil, fmt.Errorf("starting zstd: %w", err)
		}
		stop := func() {
			_ = out.Close()
			_ = cmd.Wait()
		}
		return out, stop, nil
	default:
		return nil, nil, ErrWrongPassword
	}
}

// Extract decrypts a backup into dir (which must be empty) and returns its
// Info. Only regular files with clean relative names are written.
func Extract(r io.Reader, password, dir string) (Info, error) {
	plain, err := NewReader(r, password)
	if err != nil {
		return Info{}, err
	}
	info, err := extractArchive(plain, dir)
	// When decrypting failed, the decompressor and the tar reader only saw
	// the stream end early; the decryption's own error says why.
	if rd, ok := plain.(*reader); ok && err != nil && rd.err != nil {
		return Info{}, rd.err
	}
	return info, err
}

// extractArchive writes the files of the decrypted archive into dir.
func extractArchive(plain io.Reader, dir string) (Info, error) {
	archive, stop, err := decompress(plain)
	if err != nil {
		return Info{}, err
	}
	defer stop()
	tr := tar.NewReader(archive)
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
