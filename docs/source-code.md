# Source code for everything in the image

RasBeacon's own code (`rbd`, `rbweb`, the web UI) is in this repository,
under the Apache-2.0 license.

The images also contain software from Debian and Raspberry Pi OS: the
Linux kernel, systemd, hostapd, wpa_supplicant, dnsmasq, nftables and many
more, under their own licenses (GPL, LGPL, MIT, BSD and others). Each
package's license is in the image at `/usr/share/doc/<package>/copyright`.

## Finding the exact source code

Every release includes `rasbeacon-<board>-<version>.packages.txt`, listing
each package with its **source package** and **version**, for example:

```
hostapd 2:2.10-24 source=wpa 2:2.10-24
```

To get that source:

- **Debian packages:** from the Debian archive, or its permanent snapshot
  service, which keeps every version ever released:
  `https://snapshot.debian.org/package/<source>/<version>/`. On a Debian
  computer, `apt-get source <source>=<version>` also works.
- **Raspberry Pi packages** (the kernel `linux-image-rpi-*`, firmware,
  `raspi-*` tools): from `https://archive.raspberrypi.com/debian/` (the
  `pool/` folder, by source name), or Raspberry Pi's GitHub repositories
  (`raspberrypi/linux` for the kernel).

## Written offer

If you received a RasBeacon device or image and can't get the source code
for a GPL- or LGPL-licensed part in the ways above, open an issue in this
repository (or contact the maintainers, see `SECURITY.md`) within three
years of the release, and we will provide the complete corresponding
source code for that release, at no more than the cost of providing it.

## Binary firmware

The Wi-Fi firmware (`firmware-realtek`, `firmware-mediatek`,
`firmware-brcm80211`) is distributed by Debian and Raspberry Pi under
licenses that allow redistribution but are not open source. See each
package's `copyright` file and [LICENSES/THIRD_PARTY.md](../LICENSES/THIRD_PARTY.md).
