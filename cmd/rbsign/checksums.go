package main

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"rasbeacon/internal/update"
)

// A release's SHA256SUMS is signed with the update signing key, as an
// OpenSSH file signature (`ssh-keygen -Y sign`), so anyone can check a
// download with ssh-keygen, which comes with macOS, Linux and Windows:
//
//	ssh-keygen -Y verify -f release.allowed_signers -I rasbeacon-release \
//	    -n rasbeacon-checksums -s SHA256SUMS.sig < SHA256SUMS
//
// The same key signs update manifests, as plain Ed25519 over the
// manifest's JSON. An OpenSSH signature covers "SSHSIG", its namespace and
// a hash of the file instead, which no JSON starts with, so a signed
// checksum list can never pass for a signed update.
const (
	checksumsNamespace = "rasbeacon-checksums"
	sshsigMagic        = "SSHSIG"
	sshsigHash         = "sha512"
	sshKeyType         = "ssh-ed25519"
	// maxChecksums is more than SHA256SUMS will ever be.
	maxChecksums = 64 << 10
)

// checksumLine is a line of SHA256SUMS as sha256sum writes it: the
// SHA-256 in lowercase hex, two spaces, and a release file's name.
var checksumLine = regexp.MustCompile(`^[0-9a-f]{64}  rasbeacon-[0-9A-Za-z][0-9A-Za-z._+-]*$`)

// signChecksums signs a release's SHA256SUMS with the key in
// RASBEACON_UPDATE_SIGNING_KEY, writing SHA256SUMS.sig next to it (or to
// -out). It signs only a list of checksums: the lines come from the image
// build, which never has the key, and the key signs nothing else for it.
func signChecksums(args []string, env func(string) string, stdout io.Writer) error {
	flags := flag.NewFlagSet("sign-checksums", flag.ContinueOnError)
	in := flags.String("in", "", "the release's SHA256SUMS")
	out := flags.String("out", "", "signature to write (default: the -in file with .sig added)")
	trustedDir := flags.String("trusted", "", "folder of the public keys the image trusts; the signing key must be one of them")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *in == "" {
		return errUsage
	}
	if *out == "" {
		*out = *in + ".sig"
	}
	key, err := update.ParsePrivateKey(env("RASBEACON_UPDATE_SIGNING_KEY"))
	if err != nil {
		return fmt.Errorf("RASBEACON_UPDATE_SIGNING_KEY: %w", err)
	}
	if *trustedDir != "" {
		name, err := trustedKeyName(*trustedDir, key.Public().(ed25519.PublicKey))
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Signing with the key of", name)
	}
	list, err := readChecksums(*in)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, sshSignature(key, checksumsNamespace, list), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Signed", *in)
	return nil
}

// readChecksums reads a checksum list and checks every line is one.
func readChecksums(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	list, err := io.ReadAll(io.LimitReader(f, maxChecksums+1))
	if err != nil {
		return nil, err
	}
	if len(list) > maxChecksums {
		return nil, fmt.Errorf("%s is larger than a list of checksums", path)
	}
	if err := checkChecksums(string(list)); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return list, nil
}

// checkChecksums checks a list has at least one line, each a checksum line
// for a file of its own, and ends with a newline.
func checkChecksums(list string) error {
	if list == "" || !strings.HasSuffix(list, "\n") {
		return errors.New("a list of checksums has one line per file, each ending with a newline")
	}
	seen := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSuffix(list, "\n"), "\n") {
		if !checksumLine.MatchString(line) {
			return fmt.Errorf("line %d isn't a SHA-256 checksum of a release file", i+1)
		}
		name := line[66:]
		if seen[name] {
			return fmt.Errorf("line %d: %s is listed twice", i+1, name)
		}
		seen[name] = true
	}
	return nil
}

// sshSignature signs message as `ssh-keygen -Y sign -n namespace` does,
// and returns the signature file (PROTOCOL.sshsig in OpenSSH's sources).
func sshSignature(key ed25519.PrivateKey, namespace string, message []byte) []byte {
	signature := ed25519.Sign(key, sshSignedData(namespace, message))
	blob := []byte(sshsigMagic)
	blob = binary.BigEndian.AppendUint32(blob, 1) // the format's version
	blob = appendSSHString(blob, sshPublicKey(key.Public().(ed25519.PublicKey)))
	blob = appendSSHString(blob, []byte(namespace))
	blob = appendSSHString(blob, nil) // reserved
	blob = appendSSHString(blob, []byte(sshsigHash))
	blob = appendSSHString(blob, appendSSHString(appendSSHString(nil, []byte(sshKeyType)), signature))

	encoded := base64.StdEncoding.EncodeToString(blob)
	var b strings.Builder
	b.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for len(encoded) > 70 { // ssh-keygen's line length
		b.WriteString(encoded[:70] + "\n")
		encoded = encoded[70:]
	}
	b.WriteString(encoded + "\n-----END SSH SIGNATURE-----\n")
	return []byte(b.String())
}

// sshSignedData is what an OpenSSH file signature signs: its magic, the
// namespace, and the hash of the file.
func sshSignedData(namespace string, message []byte) []byte {
	hash := sha512.Sum512(message)
	data := []byte(sshsigMagic)
	data = appendSSHString(data, []byte(namespace))
	data = appendSSHString(data, nil) // reserved
	data = appendSSHString(data, []byte(sshsigHash))
	return appendSSHString(data, hash[:])
}

// sshPublicKey is an Ed25519 public key as OpenSSH writes it (after
// "ssh-ed25519 " in a .pub file, base64-encoded).
func sshPublicKey(public ed25519.PublicKey) []byte {
	return appendSSHString(appendSSHString(nil, []byte(sshKeyType)), public)
}

// appendSSHString appends s as SSH's wire format writes a string: its
// length as 4 bytes, then s.
func appendSSHString(b, s []byte) []byte {
	return append(binary.BigEndian.AppendUint32(b, uint32(len(s))), s...)
}
