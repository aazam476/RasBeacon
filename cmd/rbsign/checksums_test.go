package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"rasbeacon/internal/update"
)

// The update key's half the image trusts, and the file people check
// releases with.
const (
	updateKeysDir  = "../../image/layer/rasbeacon.d/customize.overlay/usr/lib/rasbeacon/update-keys"
	allowedSigners = "../../docs/release.allowed_signers"
)

// checksums is a release's SHA256SUMS as the release workflow puts it
// together.
const checksums = `0f343b0931126a20f133d67c2b018a3b5cb7ec56c3d16bd5b4bea69ef8d04a9b  rasbeacon-pi3-zero2w-v0.3.0.img.xz
8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92  rasbeacon-pi3-zero2w-v0.3.0.packages.txt
4e07408562bedb8b60ce05c1decfe3ad16b72230967de01f640b7e4729b49fce  rasbeacon-v0.3.0.img.xz
4b227777d4dd1fc61c6f884f48641d02b4d121d3fd328cb08b5531fcacdabf8a  rasbeacon-v0.3.0.packages.txt
ef2d127de37b942baad06145e54b0c619a1f22327b2ebbcfbec78f5564afe39d  rasbeacon-v0.3.0.rbupdate
e7f6c011776e8db7cd330b54174fd76f7d0216b612387a5ffcfb81e6f0919683  rasbeacon-v0.3.0.update.json
`

// newKey makes a signing key with keygen, as the owner does.
func newKey(t *testing.T) (env map[string]string, public ed25519.PublicKey, output string) {
	t.Helper()
	code, out, stderr := rbsign(t, nil, "keygen")
	if code != 0 {
		t.Fatalf("keygen: %d %s", code, stderr)
	}
	private, publicText := keys(t, out)
	public, err := update.ParsePublicKey(publicText)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"RASBEACON_UPDATE_SIGNING_KEY": private}, public, out
}

// verifySSHSignature checks a signature file the way ssh-keygen -Y verify
// does, for the parts the test needs: signed by public, over message, in
// namespace.
func verifySSHSignature(t *testing.T, signature []byte, public ed25519.PublicKey, namespace string, message []byte) bool {
	t.Helper()
	text := strings.TrimSpace(string(signature))
	if !strings.HasPrefix(text, "-----BEGIN SSH SIGNATURE-----\n") || !strings.HasSuffix(text, "\n-----END SSH SIGNATURE-----") {
		t.Fatalf("not an SSH signature file:\n%s", signature)
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines[1 : len(lines)-1] {
		if len(line) > 70 {
			t.Errorf("line longer than ssh-keygen's: %q", line)
		}
	}
	blob, err := base64.StdEncoding.DecodeString(strings.Join(lines[1:len(lines)-1], ""))
	if err != nil {
		t.Fatal(err)
	}
	want := appendSSHString(nil, sshPublicKey(public))
	if !bytes.HasPrefix(blob, []byte("SSHSIG\x00\x00\x00\x01")) || !bytes.Contains(blob, want) {
		t.Fatal("the signature doesn't carry the format and the public key")
	}
	return ed25519.Verify(public, sshSignedData(namespace, message), blob[len(blob)-ed25519.SignatureSize:])
}

func TestSignChecksums_SshKeygenChecksIt(t *testing.T) {
	env, public, _ := newKey(t)
	dir := t.TempDir()
	list := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(list, []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, stderr := rbsign(t, env, "sign-checksums", "-in", list); code != 0 || !strings.Contains(out, "Signed") {
		t.Fatalf("sign-checksums: %d %s %s", code, out, stderr)
	}
	signature, err := os.ReadFile(list + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	if !verifySSHSignature(t, signature, public, checksumsNamespace, []byte(checksums)) {
		t.Error("the signature doesn't verify")
	}
	if verifySSHSignature(t, signature, public, checksumsNamespace, []byte(strings.Replace(checksums, "4e07", "4e08", 1))) {
		t.Error("the signature verifies a changed list")
	}
	// No signed checksum list passes for a signed update manifest.
	if ed25519.Verify(public, []byte(checksums), signature) {
		t.Error("the signature is over the list itself")
	}

	// The way people check a download (README.md), where OpenSSH is here.
	sshKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("no ssh-keygen here")
	}
	signers := filepath.Join(dir, "release.allowed_signers")
	if err := os.WriteFile(signers, []byte(allowedSigner(public)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	verify := func(message string) (string, error) {
		cmd := exec.Command(sshKeygen, "-Y", "verify", "-f", signers, "-I", "rasbeacon-release",
			"-n", checksumsNamespace, "-s", list+".sig")
		cmd.Stdin = strings.NewReader(message)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := verify(checksums); err != nil || !strings.Contains(out, "Good") {
		t.Errorf("ssh-keygen refused it: %v\n%s", err, out)
	}
	if out, err := verify(strings.Replace(checksums, "4e07", "4e08", 1)); err == nil {
		t.Errorf("ssh-keygen accepted a changed list:\n%s", out)
	}
}

func TestSignChecksums_SignsOnlyAListOfChecksums(t *testing.T) {
	env, _, _ := newKey(t)
	manifest := `{"version":"v99.0.0","board":"rpi64"}` + "\n"
	for name, list := range map[string]string{
		"empty":                 "",
		"no newline at the end": strings.TrimSuffix(checksums, "\n"),
		"an update manifest":    manifest,
		"a manifest after them": checksums + manifest,
		"capital letters":       strings.ToUpper(checksums[:64]) + checksums[64:],
		"one space":             strings.Replace(checksums, "  rasbeacon-v0.3.0.img.xz", " rasbeacon-v0.3.0.img.xz", 1),
		"a file elsewhere":      strings.Replace(checksums, "rasbeacon-v0.3.0.img.xz", "rasbeacon-x/../../etc/passwd", 1),
		"another file":          strings.Replace(checksums, "rasbeacon-v0.3.0.img.xz", "image.img.xz", 1),
		"a file twice":          checksums + checksums,
		"a blank line":          checksums + "\n",
		"too large":             strings.Repeat(checksums, maxChecksums/len(checksums)+1),
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "SHA256SUMS")
		if err := os.WriteFile(path, []byte(list), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := rbsign(t, env, "sign-checksums", "-in", path); code != 1 || stderr == "" {
			t.Errorf("%s: exit %d %q, want refused", name, code, stderr)
		}
		if _, err := os.Stat(path + ".sig"); err == nil {
			t.Errorf("%s: signed", name)
		}
	}
}

func TestSignChecksums_TheKeyMustBeTrusted(t *testing.T) {
	env, _, _ := newKey(t)
	dir := t.TempDir()
	list := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(list, []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
	// The release's key isn't this one.
	if code, _, stderr := rbsign(t, env, "sign-checksums", "-in", list, "-trusted", updateKeysDir); code != 1 || !strings.Contains(stderr, "none of the keys") {
		t.Errorf("an untrusted key: exit %d %q", code, stderr)
	}
	if code, _, stderr := rbsign(t, nil, "sign-checksums", "-in", list); code != 1 || !strings.Contains(stderr, "RASBEACON_UPDATE_SIGNING_KEY") {
		t.Errorf("no key: exit %d %q", code, stderr)
	}
	if _, err := os.Stat(list + ".sig"); err == nil {
		t.Error("signed")
	}
	// -out writes the signature there.
	out := filepath.Join(dir, "elsewhere.sig")
	if code, _, stderr := rbsign(t, env, "sign-checksums", "-in", list, "-out", out); code != 0 {
		t.Fatalf("-out: %d %s", code, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}
}

// keygen prints the new key's line for release.allowed_signers.
func TestKeygen_PrintsTheAllowedSignersLine(t *testing.T) {
	_, public, out := newKey(t)
	if !strings.Contains(out, allowedSigner(public)+"\n") {
		t.Errorf("keygen's output has no allowed_signers line for its key:\n%s", out)
	}
}

// docs/release.allowed_signers has a line for each key the image trusts,
// and nothing else: the releases are signed with one of them.
func TestReleaseSigners_AreTheKeysTheImageTrusts(t *testing.T) {
	data, err := os.ReadFile(allowedSigners)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	keys, err := filepath.Glob(filepath.Join(updateKeysDir, "*.pub"))
	if err != nil || len(keys) == 0 {
		t.Fatalf("no update keys: %v", err)
	}
	for _, path := range keys {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		public, err := update.ParsePublicKey(string(text))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if want := allowedSigner(public); !strings.Contains(string(data), want+"\n") {
			t.Errorf("%s has no line for %s; add:\n%s", allowedSigners, filepath.Base(path), want)
		}
	}
	if len(lines) != len(keys) {
		t.Errorf("%s has %d keys, the image trusts %d", allowedSigners, len(lines), len(keys))
	}
}
