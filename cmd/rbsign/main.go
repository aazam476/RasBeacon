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
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

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
