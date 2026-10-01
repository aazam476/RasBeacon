# RasBeacon

An open-source, battery-powered Wi-Fi travel router for the Raspberry Pi.
Your devices join RasBeacon's hotspot, and RasBeacon joins the network where
you are (hotel, campus eduroam, café). The goal is that all your traffic can
go through your own WireGuard VPN, with a kill switch and an easy way through
hotel login pages.

- **Setup happens in your phone's browser.** No command line, no compiling.
- **No shared default passwords.** Every device generates its own on first
  boot.
- **No telemetry and no cloud.** Everything works offline.
- **Licensed under Apache-2.0.**

> **Status: all six milestones are built and tested in CI; hardware
> testing is in progress** (see [docs/testing/](docs/testing/)). RasBeacon
> runs your hotspot, connects it to the internet through Ethernet or Wi-Fi
> (open, password, or WPA-Enterprise such as eduroam,
> [guide](docs/eduroam.md)), can send everything through your own WireGuard
> VPN with a kill switch ([server guide](docs/wireguard-server.md)), lets
> you log in to hotel login pages without turning the kill switch off, and
> installs signed updates with automatic rollback.

## What you need

| Part | Notes |
|---|---|
| Raspberry Pi 4 or 5 | A Pi 4 may need a one-time bootloader update first (see [Troubleshooting](#troubleshooting): 4 short flashes). The Pi Zero 2 W and older boards don't work with the current image. See [docs/compatibility.md](docs/compatibility.md). |
| A USB Wi-Fi adapter that can run a hotspot | Recommended: one with a **MediaTek MT7612U** chip (e.g. Alfa AWUS036ACM). A TP-Link Archer T3U also works in principle. The Pi's built-in Wi-Fi alone holds only about 8 devices. |
| microSD card, 8 GB or larger | |
| USB power bank, about 20,000 mAh for a full day on a Pi 4/5 | Or any 5 V / 3 A USB-C supply |

## Set up (about 15 minutes)

1. **Download** `rasbeacon-pi4-<version>.img.xz` (for a Pi 4 or 400) or `rasbeacon-pi5-<version>.img.xz` (for a Pi 5) from the
   [Releases](../../releases) page. Also download `SHA256SUMS` if you want to
   check the file (see [Checking your download](#checking-your-download)).
2. **Flash** it with [Raspberry Pi Imager](https://www.raspberrypi.com/software/):
   1. Choose your device.
   2. Under *Operating System*, pick **Use custom** and select the downloaded
      file.
   3. Choose your SD card.
   4. When asked about OS customisation, choose **No**: RasBeacon is set up
      from your phone instead.

   [balenaEtcher](https://etcher.balena.io/) works too.
3. **Optional, recommended in crowded places:** after flashing, the card
   shows up on your computer as a small drive called `BOOTCONFIG`. Create a
   plain text file named `setup.txt` on it containing:
   ```
   setup_password=choose-a-password
   ```
   (8 to 63 characters.) The setup network will then use this password
   instead of being open.
4. **Assemble:** put the card in the Pi, plug the USB Wi-Fi adapter into a
   USB port, then connect power. The first boot takes about a minute.
5. **Join the setup network** from your phone. It is called
   `RasBeacon-Setup-XXXX`, where XXXX is unique to your device. Your phone
   should open the setup page by itself. If it doesn't, open
   `http://10.87.0.1` in the browser.
6. **Finish setup:**
   1. Choose an admin password (for these settings).
   2. Choose your hotspot's name and Wi-Fi password (a random one is
      suggested).
   3. Choose the band: 2.4 GHz works with every device; 5 GHz is faster but
      has shorter range.
7. After a few seconds the setup network disappears and **your hotspot
   starts**. Join it, then open `http://10.87.0.1` (or
   `http://rasbeacon.lan`) to see the status page.
8. **Connect RasBeacon to the internet:** plug in an Ethernet cable, or open
   the **Internet** page, tap *Look for nearby networks*, pick the network,
   enter its password, *Add to list*, and *Save*. You can save several
   networks; RasBeacon joins the first one in the list that is in range,
   and prefers a plugged-in cable.
9. **Optional, your VPN:** on the **VPN** page, import your WireGuard
   `.conf` file and tick *Use the VPN*. Keep the *Kill switch* on so nothing
   is ever sent outside the VPN.

> **Important:** the setup network closes after 30 minutes if nobody
> finishes setup. Unplug and replug the power to reopen it. Until setup is
> done, whoever finishes it first owns the device, which is why `setup.txt`
> is recommended in busy places.

## Troubleshooting

| What you see | What it means and what to do |
|---|---|
| No `RasBeacon-Setup-…` network after 2 minutes, and the green LED blinks **3 short, 1 long**, repeating | `setup.txt` exists but can't be used. Common causes: the password is shorter than 8 or longer than 63 characters, or has accented letters or emoji; the line doesn't start with `setup_password=`; or there is something else in the file. Fix or delete the file on the `BOOTCONFIG` drive and power-cycle. |
| No setup network, and the green LED repeats **4 short flashes** | The Pi 4's bootloader (firmware stored on the board) is too old to start RasBeacon's A/B layout. Update it once: in Raspberry Pi Imager choose your Pi 4, then *OS → Misc utility images → Bootloader → SD Card Boot*, write it to any SD card, boot the Pi with it until the green LED blinks steadily (green screen on a monitor), then reflash RasBeacon. Pi 5s ship with a new enough bootloader. |
| No setup network, and another repeating pattern of long/short flashes | A boot error. Count the long and short flashes and look them up in the [Raspberry Pi LED code table](https://www.raspberrypi.com/documentation/computers/configuration.html#led-warning-flash-codes). |
| No setup network and no blinking | Check the USB Wi-Fi adapter is plugged in, and power-cycle. If it still doesn't appear, connect the Pi to a TV and keyboard, or see [docs/testing/m1.md](docs/testing/m1.md) for how to collect logs. |
| Devices on the hotspot say "No internet" | Check the **Internet** section of the Status page. "Not connected": plug in a cable or save a Wi-Fi network on the Internet page. Wi-Fi "Looking for your saved networks": the network is out of range or its password is wrong. If the network shows a login page (hotels), that arrives in milestone 5. |
| Hotel or café Wi-Fi with a login page, and the VPN's kill switch is on | Your phone should show RasBeacon's *This network needs a login* page by itself (or open `http://10.87.0.1/#portal`). Tap **Start login mode**, log in on the network's page, and RasBeacon turns the VPN back on. During login mode (at most 10 minutes, extendable to 30), web traffic from your devices skips the VPN. |
| A warning says the Ethernet port "seems to be plugged into a network" | The port is set to *Client port* (for your own laptop), but it's plugged into a network. Set it to *Uplink* on the Internet page if that cable is your internet connection. |
| The status page warns that the built-in Wi-Fi is running the hotspot | RasBeacon didn't find a USB adapter it can use. Plug one in and restart from the System page. |
| Nothing works and you want to see why | Put the SD card in a computer and create an empty file named `debug` on the `BOOTCONFIG` drive. Boot the Pi and wait 3 minutes, then unplug it. Back on the computer, open `rasbeacon-debug.txt` from the same drive. It holds the startup logs (no passwords, but device names and MAC addresses, so delete it afterwards). |
| Forgot the admin password | Factory reset: put the SD card in a computer and create an empty file named `factory-reset` on the `BOOTCONFIG` drive. At the next start, RasBeacon erases all settings and runs first-time setup again. (Or hold the optional reset button for 10 seconds.) |
| An update didn't install | The System page says why. "not signed by a key this RasBeacon trusts": use update files from the official Releases page; images built before signing was set up can't install updates, so flash the new image once. "for pi5, but this is a pi4": download the file for your board. |

## Checking your download

Each release lists the SHA-256 checksum of every image in `SHA256SUMS`.

- **macOS/Linux:** `shasum -a 256 rasbeacon-pi4-v0.1.0.img.xz`
- **Windows PowerShell:** `Get-FileHash rasbeacon-pi4-v0.1.0.img.xz`

The result must match the line in `SHA256SUMS`.

## Updating

On the **System** page, choose the release's `rasbeacon-<board>-<version>.rbupdate`
file and tap **Install update**. RasBeacon checks that the file was signed
by the RasBeacon release key, installs it next to the running version,
and restarts into it. If the new version doesn't start correctly, it goes
back to the old one by itself. Your settings are kept. (You can also turn
on a check for new versions there; it is off by default.)

## Optional extras

- **HTTPS** for the settings pages: the **HTTPS** page. Your browser warns
  about RasBeacon's own certificate the first time; compare the
  fingerprint shown on that page.
- **SSH** for advanced users: the **SSH** page (keys only, hotspot side
  only).
- **Factory-reset button:** connect a push button between pin 11 (GPIO 17)
  and pin 9 (ground). Holding it for 10 seconds erases all settings.

## For contributors

- **How it's built:** [CONTRIBUTING.md](CONTRIBUTING.md) covers building,
  testing, running a simulated device on your computer, and code style.
- **How it works:** [docs/architecture.md](docs/architecture.md), with a
  [glossary](docs/glossary.md) of networking terms.
- **Design and decisions:** [docs/design.md](docs/design.md) and
  [docs/decisions/](docs/decisions/).
- **Web UI and API:** [docs/api.md](docs/api.md) and
  [docs/ui-contract.md](docs/ui-contract.md).
- **Releases and update signing:** [docs/releasing.md](docs/releasing.md).
- **Security problems:** please report them privately, see
  [SECURITY.md](SECURITY.md).

## License

Apache-2.0, see [LICENSE](LICENSE). The images also contain Debian and
Raspberry Pi OS packages under their own licenses (GPL, LGPL, MIT and others)
and binary Wi-Fi firmware. See
[LICENSES/THIRD_PARTY.md](LICENSES/THIRD_PARTY.md) and, for getting their
source code, [docs/source-code.md](docs/source-code.md).
