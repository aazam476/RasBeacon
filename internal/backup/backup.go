// Package backup reads and writes RasBeacon's backup files (.rbbackup): the
// settings and the plugins, as a tar archive compressed with zstd (gzip
// before RasBeacon 0.2) and encrypted with a password the owner chooses.
//
// # The file
//
//	"RBBACKUP" 0x00 0x01         magic and format version (10 bytes)
//	uint32 length, header JSON   the key derivation's salt and costs
//	chunks...                    the encrypted archive
//
// The key is derived from the password with Argon2id. The archive is cut
// into 64 KB chunks, each encrypted with AES-256-GCM: chunk n uses the nonce
// n (big-endian, 12 bytes), and the last chunk is marked in its additional
// data, so chunks can't be reordered, dropped or cut off unnoticed. Each
// chunk is stored as uint32 length + ciphertext.
package backup

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Magic starts every backup file.
var Magic = []byte("RBBACKUP\x00\x01")

// chunkSize is how much of the archive each encrypted chunk holds.
const chunkSize = 64 << 10

// MinPasswordLength is the shortest password a backup may have.
const MinPasswordLength = 8

// ErrWrongPassword is returned when the password doesn't open the file (or
// the file was changed).
var ErrWrongPassword = errors.New("the password is wrong, or the backup file is damaged")

// ErrIncomplete is returned for a backup that ends before its last chunk:
// it was cut off while it was being saved (the download stopped, or the
// disk filled up).
var ErrIncomplete = errors.New("the backup file is incomplete: it was cut off while it was saved")

// ErrNotBackup is returned for a file that isn't a RasBeacon backup.
var ErrNotBackup = errors.New("this is not a RasBeacon backup (.rbbackup) file")

// header is the file's clear-text header.
type header struct {
	KDF     string `json:"kdf"`
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memoryKiB"`
	Threads uint8  `json:"threads"`
}

// The key derivation's costs: about a second on a Raspberry Pi Zero 2 W,
// and 64 MB of memory (it has 512 MB).
const (
	kdfTime    = 3
	kdfMemory  = 64 * 1024
	kdfThreads = 2
)

// key derives the 32-byte AES key from the password with Argon2id, using
// the header's salt and costs.
func (h header) key(password string) []byte {
	return argon2.IDKey([]byte(password), h.Salt, h.Time, h.Memory, h.Threads, 32)
}

// writer encrypts chunk by chunk.
type writer struct {
	w       io.Writer
	aead    cipher.AEAD
	buf     []byte
	counter uint64
	closed  bool
}

// NewWriter starts an encrypted backup file on w. Write the archive to it,
// then Close it (which writes the last chunk; it doesn't close w).
func NewWriter(w io.Writer, password string) (io.WriteCloser, error) {
	if len(password) < MinPasswordLength {
		return nil, fmt.Errorf("the password must be at least %d characters", MinPasswordLength)
	}
	h := header{KDF: "argon2id", Salt: make([]byte, 16), Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads}
	if _, err := rand.Read(h.Salt); err != nil {
		return nil, err
	}
	aead, err := newAEAD(h.key(password))
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(h)
	if _, err := w.Write(Magic); err != nil {
		return nil, err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	return &writer{w: w, aead: aead, buf: make([]byte, 0, chunkSize)}, nil
}

// newAEAD makes the AES-256-GCM cipher: it encrypts a chunk and adds a tag
// that proves nothing in it changed.
func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// nonce is the 12-byte nonce for chunk number counter. Each chunk gets its
// own, and the counter also fixes the chunks' order.
func nonce(counter uint64) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n[4:], counter)
	return n
}

// additional data: whether this is the last chunk.
var (
	middleChunk = []byte{0}
	lastChunk   = []byte{1}
)

// Write collects the archive into 64 KB chunks and encrypts each full one.
func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("backup writer is closed")
	}
	written := 0
	for len(p) > 0 {
		n := min(len(p), chunkSize-len(w.buf))
		w.buf = append(w.buf, p[:n]...)
		p = p[n:]
		written += n
		// A full chunk is only sealed once more follows, so the last one
		// can be marked as last in Close.
		if len(w.buf) == chunkSize && len(p) > 0 {
			if err := w.seal(false); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

// seal encrypts the collected chunk and writes it as length + ciphertext.
func (w *writer) seal(last bool) error {
	ad := middleChunk
	if last {
		ad = lastChunk
	}
	sealed := w.aead.Seal(nil, nonce(w.counter), w.buf, ad)
	w.counter++
	w.buf = w.buf[:0]
	if err := binary.Write(w.w, binary.BigEndian, uint32(len(sealed))); err != nil {
		return err
	}
	_, err := w.w.Write(sealed)
	return err
}

// Close writes the last chunk.
func (w *writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.seal(true)
}

// reader decrypts chunk by chunk.
type reader struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	plain   []byte
	counter uint64
	done    bool
	// err is the first error opening a chunk. Extract reports it instead
	// of what the decompressor or tar reader made of the stream ending.
	err error
}

// NewReader opens an encrypted backup file. A wrong password fails here
// (the first chunk doesn't open).
func NewReader(r io.Reader, password string) (io.Reader, error) {
	br := bufio.NewReaderSize(r, chunkSize+64)
	magic := make([]byte, len(Magic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != string(Magic) {
		return nil, ErrNotBackup
	}
	var length uint32
	if err := binary.Read(br, binary.BigEndian, &length); err != nil || length > 4096 {
		return nil, ErrNotBackup
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(br, data); err != nil {
		return nil, ErrNotBackup
	}
	// The header isn't encrypted, so its costs are checked before use: a
	// made-up file must not make the Pi spend minutes or all its memory on
	// the key.
	var h header
	if err := json.Unmarshal(data, &h); err != nil || h.KDF != "argon2id" || len(h.Salt) < 16 ||
		h.Time == 0 || h.Time > 10 || h.Memory < 8*1024 || h.Memory > 256*1024 || h.Threads == 0 || h.Threads > 4 {
		return nil, ErrNotBackup
	}
	aead, err := newAEAD(h.key(password))
	if err != nil {
		return nil, err
	}
	rd := &reader{r: br, aead: aead}
	if err := rd.next(); err != nil {
		return nil, err
	}
	return rd, nil
}

// next opens the next chunk, remembering the first error in rd.err.
func (rd *reader) next() error {
	if rd.err == nil {
		rd.err = rd.open()
	}
	return rd.err
}

// open reads and decrypts the next chunk. A file that ends before its last
// chunk, or in the middle of one, was cut off (ErrIncomplete): a wrong
// password never makes the file shorter. A chunk that doesn't decrypt is a
// wrong password or a changed file, which look the same.
func (rd *reader) open() error {
	var length uint32
	if err := binary.Read(rd.r, binary.BigEndian, &length); err != nil {
		return ErrIncomplete // the file ends before the last chunk
	}
	if length > chunkSize+uint32(rd.aead.Overhead()) {
		return ErrWrongPassword
	}
	sealed := make([]byte, length)
	if _, err := io.ReadFull(rd.r, sealed); err != nil {
		return ErrIncomplete // the file ends inside a chunk
	}
	n := nonce(rd.counter)
	rd.counter++
	if plain, err := rd.aead.Open(nil, n, sealed, middleChunk); err == nil {
		rd.plain = plain
		return nil
	}
	plain, err := rd.aead.Open(nil, n, sealed, lastChunk)
	if err != nil {
		return ErrWrongPassword
	}
	rd.plain, rd.done = plain, true
	// Nothing may follow the last chunk.
	if _, err := rd.r.ReadByte(); err != io.EOF {
		return ErrWrongPassword
	}
	return nil
}

// Read hands out the decrypted archive, opening chunks as needed.
func (rd *reader) Read(p []byte) (int, error) {
	for len(rd.plain) == 0 {
		if rd.done {
			return 0, io.EOF
		}
		if err := rd.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, rd.plain)
	rd.plain = rd.plain[n:]
	return n, nil
}
