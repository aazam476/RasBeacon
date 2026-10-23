# Third-party components and redistribution

RasBeacon's own code (this repository) is licensed under **Apache-2.0**
(see [../LICENSE](../LICENSE)). Three things were copied from other
projects: Cloudflare's speed test library, unchanged, with its license next
to it in `internal/speedtest/cloudflare/`, the outlines of the Material
Symbols icons the web UI uses (`web/src/ui/icons.ts`), and, for the tests
only, the Wi-Fi regulatory database (`internal/regdb/testdata/`, with its
license next to it). All are listed below.

The released SD card images also contain software and firmware from others.
This file lists what, and what redistributing the images requires.

## In the images

| Component | Source | License |
|---|---|---|
| Debian trixie packages (Linux userland, systemd, hostapd, dnsmasq, nftables, OpenSSH, ...) | deb.debian.org | Various: GPL-2.0, GPL-3.0, LGPL, BSD, MIT, and others. Each package's terms are in `/usr/share/doc/<package>/copyright` inside the image. |
| Raspberry Pi kernel, boot firmware and packages | archive.raspberrypi.com | Kernel: GPL-2.0. Boot firmware: Broadcom redistributable binary license. |
| Wi-Fi firmware for the built-in radio (Cypress/Synaptics `brcmfmac`) | `firmware-brcm80211` package | Binary redistribution license, **to verify (milestone 6)** |
| Wi-Fi firmware for MediaTek MT76 USB adapters | `linux-firmware` | MediaTek redistribution license, **to verify** |
| Wi-Fi firmware for Realtek RTW88 USB adapters | `linux-firmware` | Realtek redistribution license, **to verify** |
| React, React DOM, React Router (in the web UI) | npm | MIT |
| Tailwind CSS base styles (in the web UI's built CSS) | npm | MIT |
| Material Symbols Rounded icon shapes (SVG paths in `web/src/ui/icons.ts`, in the web UI) | fonts.google.com/icons (github.com/google/material-design-icons) | Apache-2.0 |
| golang.org/x/crypto (in rbd and rbweb) | Go module | BSD-3-Clause |
| rsc.io/qr 0.2.0 (QR codes, in rbd) | Go module | BSD-3-Clause |
| Cloudflare speed test library 1.14.1 (in rbd, run in the headless browser) | github.com/cloudflare/speedtest (npm `@cloudflare/speedtest`) | MIT (`internal/speedtest/cloudflare/LICENSE`) |
| Go standard library (in rbd and rbweb) | go.dev | BSD-3-Clause |
| Wi-Fi regulatory database (`wireless-regdb` package in the images; a copy in `internal/regdb/testdata/` for the tests) | git.kernel.org wireless-regdb | ISC |

The build tools (rpi-image-gen, Vite, TypeScript, Tailwind CSS, oxlint,
Prettier, Vitest, Playwright, ...)
are used to build or test RasBeacon. They are not shipped in the images.

## Obligations when redistributing the images

1. **Corresponding source for GPL and LGPL packages.** Each release
   includes `rasbeacon-<board>-<version>.packages.txt`, which lists every
   package in the image with its source package and exact version. The
   sources are in the public Debian and Raspberry Pi archives at those
   versions (snapshot.debian.org keeps every version), and a written offer
   covers anyone who can't get them there. See
   [docs/source-code.md](../docs/source-code.md).
2. **Keep the copyright files.** The images keep every package's
   `/usr/share/doc/*/copyright`.
3. **Firmware:** keep the firmware license files that ship with the firmware
   packages.
4. **Trademarks:** the image is "RasBeacon, based on Debian and Raspberry Pi
   OS packages", not "Raspberry Pi OS".
