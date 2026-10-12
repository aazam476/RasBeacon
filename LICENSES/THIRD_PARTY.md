# Third-party components and redistribution

RasBeacon's own code (this repository) is licensed under **Apache-2.0**
(see [../LICENSE](../LICENSE)). One file was copied from another project,
unchanged: Cloudflare's speed test library (below), with its license next
to it in `internal/speedtest/cloudflare/`.

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
| React, React DOM (in the web UI) | npm | MIT |
| golang.org/x/crypto (in rbd and rbweb) | Go module | BSD-3-Clause |
| Cloudflare speed test library 1.14.1 (in rbd, run in the headless browser) | github.com/cloudflare/speedtest (npm `@cloudflare/speedtest`) | MIT (`internal/speedtest/cloudflare/LICENSE`) |
| Go standard library (in rbd and rbweb) | go.dev | BSD-3-Clause |

The build tools (rpi-image-gen, Vite, TypeScript, ESLint, Playwright, ...)
are used to build or test RasBeacon. They are not shipped in the images.

## Obligations when redistributing the images

1. **Corresponding source for GPL and LGPL packages.** From milestone 6,
   each release will include a `sources-<version>.tar` built from the exact
   package versions in the image. Until then, test images are built from
   public Debian and Raspberry Pi archives at the versions recorded in the
   image's package list.
2. **Keep the copyright files.** The images keep every package's
   `/usr/share/doc/*/copyright`.
3. **Firmware:** keep the firmware license files that ship with the firmware
   packages.
4. **Trademarks:** the image is "RasBeacon, based on Debian and Raspberry Pi
   OS packages", not "Raspberry Pi OS".
