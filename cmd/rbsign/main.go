// Command rbsign makes the signing key for RasBeacon updates and signs
// update files at release time. See docs/releasing.md.
//
//	rbsign keygen
//	    Prints a new private key (keep it secret: it goes into the
//	    RASBEACON_UPDATE_SIGNING_KEY secret of the GitHub repository) and its
//	    public key (commit it to image/.../usr/lib/rasbeacon/update-keys/).
//
//	rbsign pack -board rpi64 -version v1.2.0 [-min-from v1.0.0] -bundle x.update.tar.zst -out x.rbupdate [-info x.update.json] [-trusted DIR]
//	    Makes a signed update file. The private key is read from the
//	    RASBEACON_UPDATE_SIGNING_KEY environment variable, never from the
//	    command line (which other users of the computer could see).
//	    -min-from names the oldest version that can install it (none: any).
//	    -info also writes a small JSON file with the version and -min-from,
//	    which the online update check reads from the release.
//	    -trusted names the folder of public keys the image trusts: pack
//	    then says which of them belongs to the signing key, and refuses to
//	    sign when none does (devices would refuse such an update).
//
//	rbsign plugin-keygen
//	    Prints a new key for signing plugins: the private key (keep it
//	    secret, e.g. as the RASBEACON_PLUGIN_SIGNING_KEY secret of the
//	    plugins' repository) and its public key. See docs/plugins.md.
//
//	rbsign plugin-sign -in plugin.rba -out signed.rba
//	    Signs a plugin file with the key in RASBEACON_PLUGIN_SIGNING_KEY.
//	    RasBeacon remembers the key at the first install and accepts updates
//	    only when they are signed by the same key.
//
//	rbsign plugin-check FILE.rba...
//	    Checks plugin files the way RasBeacon does before installing them,
//	    and prints what each is and asks for.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rasbeacon/internal/plugin"
	"rasbeacon/internal/update"
)

// main runs the command named by the first argument (see the package
// comment for each one). The work is in run, so that tests can call it.
func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// errUsage means the command line was wrong: run prints how to call
// rbsign and exits with 2, as command-line tools usually do.
var errUsage = errors.New("usage")

// run runs one rbsign command and returns the exit code: 0 when it worked,
// 1 when it failed, 2 when the command line was wrong. env reads an
// environment variable (os.Getenv, or a fake in tests); output goes to
// stdout and stderr.
func run(args []string, env func(string) string, stdout, stderr io.Writer) int {
	err := errUsage
	if len(args) > 0 {
		switch args[0] {
		case "keygen":
			err = keygen(stdout)
		case "pack":
			err = pack(args[1:], env, stdout)
		case "plugin-keygen":
			err = pluginKeygen(stdout)
		case "plugin-sign":
			err = pluginSign(args[1:], env, stdout)
		case "plugin-check":
			err = pluginCheck(args[1:], stdout)
		}
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintln(stderr, "usage: rbsign keygen | rbsign pack -board B -version V -bundle FILE -out FILE")
		fmt.Fprintln(stderr, "       rbsign plugin-keygen | rbsign plugin-sign -in FILE.rba -out FILE.rba | rbsign plugin-check FILE.rba...")
		return 2
	default:
		fmt.Fprintln(stderr, "rbsign:", err)
		return 1
	}
}

// parseFlags reads a command's flags. A flag rbsign doesn't know is a
// usage error.
func parseFlags(flags *flag.FlagSet, args []string) error {
	flags.SetOutput(io.Discard) // run prints the usage itself
	if err := flags.Parse(args); err != nil {
		return errUsage
	}
	return nil
}

// keygen prints a new key pair for signing RasBeacon updates.
func keygen(stdout io.Writer) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Private key (store as the GitHub secret RASBEACON_UPDATE_SIGNING_KEY; never commit it):")
	fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(private.Seed()))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Public key (save as image/layer/rasbeacon.d/customize.overlay/usr/lib/rasbeacon/update-keys/<name>.pub):")
	fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(public))
	return nil
}

// pack makes a signed update file from a RAUC bundle.
func pack(args []string, env func(string) string, stdout io.Writer) error {
	flags := flag.NewFlagSet("pack", flag.ContinueOnError)
	board := flags.String("board", "", "board the update is for: rpi64 (the Pi 4 and Pi 5 image)")
	version := flags.String("version", "", "version, e.g. v1.2.0")
	bundlePath := flags.String("bundle", "", "rpi-image-gen's <image>.update.tar.zst")
	outPath := flags.String("out", "", "update file to write (.rbupdate)")
	minFrom := flags.String("min-from", "", "oldest version that can install the update (empty: any)")
	infoPath := flags.String("info", "", "also write the version and -min-from to this JSON file")
	trustedDir := flags.String("trusted", "", "folder of the public keys the image trusts; the signing key must be one of them")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *board == "" || *version == "" || *bundlePath == "" || *outPath == "" {
		return errUsage
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
	bundle, err := os.Open(*bundlePath)
	if err != nil {
		return err
	}
	defer bundle.Close()
	out, err := os.Create(*outPath)
	if err != nil {
		return err
	}
	if err := update.Pack(out, bundle, *board, *version, *minFrom, key); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if *infoPath == "" {
		return nil
	}
	info, err := json.MarshalIndent(update.Info{Version: *version, MinFromVersion: *minFrom}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*infoPath, append(info, '\n'), 0o644)
}

// trustedKeyName returns the name of the file in dir that holds public,
// the signing key's public half. It's safe to print: public keys are
// public. None of them: devices with this image would refuse the update.
func trustedKeyName(dir string, public ed25519.PublicKey) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return "", err
		}
		if key, err := update.ParsePublicKey(string(data)); err == nil && key.Equal(public) {
			return entry.Name(), nil
		}
	}
	return "", fmt.Errorf("the signing key (public key %s) is none of the keys in %s: RasBeacon would refuse its updates",
		base64.StdEncoding.EncodeToString(public), dir)
}

// pluginKeygen prints a new key pair for signing plugins, and the public
// key's ID as RasBeacon shows it.
func pluginKeygen(stdout io.Writer) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Private key (store as the secret RASBEACON_PLUGIN_SIGNING_KEY; never commit it):")
	fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(private.Seed()))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Public key (RasBeacon shows its ID when installing your plugins):")
	fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(public))
	fmt.Fprintln(stdout, "Key ID:", plugin.KeyID(public))
	return nil
}

// pluginSign signs a .rba with the key in RASBEACON_PLUGIN_SIGNING_KEY,
// then opens the result the way RasBeacon will.
func pluginSign(args []string, env func(string) string, stdout io.Writer) error {
	flags := flag.NewFlagSet("plugin-sign", flag.ContinueOnError)
	in := flags.String("in", "", "plugin file to sign (.rba)")
	out := flags.String("out", "", "signed plugin file to write (.rba)")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *in == "" || *out == "" {
		return errUsage
	}
	key, err := update.ParsePrivateKey(env("RASBEACON_PLUGIN_SIGNING_KEY"))
	if err != nil {
		return fmt.Errorf("RASBEACON_PLUGIN_SIGNING_KEY: %w", err)
	}
	if err := plugin.Sign(*in, *out, key); err != nil {
		return err
	}
	// Check the result the way RasBeacon will.
	a, err := plugin.Open(*out)
	if err != nil {
		return fmt.Errorf("the signed file doesn't open: %w", err)
	}
	fmt.Fprintf(stdout, "Signed %s %s (key ID %s)\n", a.Manifest.ID, a.Manifest.Version, plugin.KeyID(a.PublicKey))
	return nil
}

// pluginCheck opens each .rba the way RasBeacon does before installing
// it, and prints what it is and asks for.
func pluginCheck(files []string, stdout io.Writer) error {
	if len(files) == 0 {
		return errUsage
	}
	for _, file := range files {
		a, err := plugin.Open(file)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		m := a.Manifest
		signed := "not signed"
		if a.PublicKey != nil {
			signed = "signed, key ID " + plugin.KeyID(a.PublicKey)
		}
		image := "image.tar"
		if m.Image != nil {
			image = m.Image.Ref + "@" + m.Image.Digest
		}
		fmt.Fprintf(stdout, "%s: %s %s (%s), %s, permissions %v, image %s\n", file, m.ID, m.Version, m.Name, signed, m.Permissions, image)
	}
	return nil
}
