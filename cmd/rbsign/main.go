// Command rbsign makes the signing key for RasBeacon updates and signs
// update files at release time. See docs/releasing.md.
//
//	rbsign keygen
//	    Prints a new private key (keep it secret: it goes into the
//	    RASBEACON_UPDATE_SIGNING_KEY secret of the GitHub repository) and its
//	    public key (commit it to image/.../usr/lib/rasbeacon/update-keys/).
//
//	rbsign pack -board rpi64 -version v1.2.0 -bundle x.update.tar.zst -out x.rbupdate
//	    Makes a signed update file. The private key is read from the
//	    RASBEACON_UPDATE_SIGNING_KEY environment variable, never from the
//	    command line (which other users of the computer could see).
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
	"flag"
	"fmt"
	"os"

	"rasbeacon/internal/plugin"
	"rasbeacon/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen()
	case "pack":
		err = pack(os.Args[2:])
	case "plugin-keygen":
		err = pluginKeygen()
	case "plugin-sign":
		err = pluginSign(os.Args[2:])
	case "plugin-check":
		err = pluginCheck(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rbsign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: rbsign keygen | rbsign pack -board B -version V -bundle FILE -out FILE")
	fmt.Fprintln(os.Stderr, "       rbsign plugin-keygen | rbsign plugin-sign -in FILE.rba -out FILE.rba | rbsign plugin-check FILE.rba...")
	os.Exit(2)
}

func keygen() error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Println("Private key (store as the GitHub secret RASBEACON_UPDATE_SIGNING_KEY; never commit it):")
	fmt.Println(base64.StdEncoding.EncodeToString(private.Seed()))
	fmt.Println()
	fmt.Println("Public key (save as image/layer/rasbeacon.d/customize.overlay/usr/lib/rasbeacon/update-keys/<name>.pub):")
	fmt.Println(base64.StdEncoding.EncodeToString(public))
	return nil
}

func pack(args []string) error {
	flags := flag.NewFlagSet("pack", flag.ExitOnError)
	board := flags.String("board", "", "board the update is for: rpi64 (the Pi 4 and Pi 5 image)")
	version := flags.String("version", "", "version, e.g. v1.2.0")
	bundlePath := flags.String("bundle", "", "rpi-image-gen's <image>.update.tar.zst")
	outPath := flags.String("out", "", "update file to write (.rbupdate)")
	_ = flags.Parse(args)
	if *board == "" || *version == "" || *bundlePath == "" || *outPath == "" {
		usage()
	}

	key, err := update.ParsePrivateKey(os.Getenv("RASBEACON_UPDATE_SIGNING_KEY"))
	if err != nil {
		return fmt.Errorf("RASBEACON_UPDATE_SIGNING_KEY: %w", err)
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
	if err := update.Pack(out, bundle, *board, *version, key); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func pluginKeygen() error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Println("Private key (store as the secret RASBEACON_PLUGIN_SIGNING_KEY; never commit it):")
	fmt.Println(base64.StdEncoding.EncodeToString(private.Seed()))
	fmt.Println()
	fmt.Println("Public key (RasBeacon shows its ID when installing your plugins):")
	fmt.Println(base64.StdEncoding.EncodeToString(public))
	fmt.Println("Key ID:", plugin.KeyID(public))
	return nil
}

func pluginSign(args []string) error {
	flags := flag.NewFlagSet("plugin-sign", flag.ExitOnError)
	in := flags.String("in", "", "plugin file to sign (.rba)")
	out := flags.String("out", "", "signed plugin file to write (.rba)")
	_ = flags.Parse(args)
	if *in == "" || *out == "" {
		usage()
	}
	key, err := update.ParsePrivateKey(os.Getenv("RASBEACON_PLUGIN_SIGNING_KEY"))
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
	fmt.Printf("Signed %s %s (key ID %s)\n", a.Manifest.ID, a.Manifest.Version, plugin.KeyID(a.PublicKey))
	return nil
}

func pluginCheck(files []string) error {
	if len(files) == 0 {
		usage()
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
		fmt.Printf("%s: %s %s (%s), %s, permissions %v, image %s\n", file, m.ID, m.Version, m.Name, signed, m.Permissions, image)
	}
	return nil
}
