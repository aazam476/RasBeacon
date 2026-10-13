# Hardware compatibility

**Support tiers:**
- **Recommended (tier 1): Pi 4 and Pi 5.** Everything works, including
  updates installed from the System page. Tested before each release; a
  problem here blocks a release.
- **Best effort (tier 2): Pi Zero 2 W, 3B and 3B+.** The same features,
  except that new versions are flashed (one copy of the system), plus each
  board's hardware limits below. Tested when someone can; a problem here is
  fixed in a following release rather than blocking one.

**Status meanings:**
- **Tested:** someone ran the listed checks on real hardware and recorded
  the numbers.
- **Untested:** expected to work from specifications or driver support, but
  not yet checked.
- **Known-limited** or **known-bad:** tried, with the problem described.

Please add your results. See
[CONTRIBUTING.md](../CONTRIBUTING.md#adding-hardware-to-the-compatibility-table).
Sources for figures not measured by us are listed in
[decisions/0001-platform.md](decisions/0001-platform.md).

## Boards

| Board | Image | Status | Notes |
|---|---|---|---|
| Raspberry Pi 4 Model B (2 GB+) | `rasbeacon` | **Boots, setup and hotspot work** (owner's Pi 4, milestone 1 image, built-in radio as hotspot; USB adapter, uplink, VPN and update checks pending). **Needs a recent bootloader:** an older one ignores the A/B selection and blinks 4 short flashes ("start*.elf not found"), reported by the owner on first try. Fix: Imager → Misc utility images → Bootloader → SD Card Boot. | Built-in Wi-Fi: CYW43455, 2.4/5 GHz (uplink). Gigabit Ethernet. Published idle figures range from about 1.4 W to 2.9 W depending on setup: measure yours. |
| Raspberry Pi 5 | `rasbeacon` | Untested (reference) | Runs the same 64-bit kernel as the Pi 4 (`kernel8.img`, 4 KB pages; set in `config.txt`), not its own 16 KB-page one. On power banks (no 5 V/5 A USB-PD), the Pi 5 limits all USB ports to 600 mA combined. One MT7612U adapter (about 380 mA under load) fits. |
| Raspberry Pi Zero 2 W | `rasbeacon-pi3-zero2w` | Untested (best effort) | Shares the second image with the Pi 3B and 3B+: the boot ROM only reads an MBR partition table and can't follow the Pi 4/5 A/B selection, so this image has one writable system partition (same programs and kernel). New versions are flashed (no in-place updates). No Ethernet port (the Ethernet settings don't show). Built-in Wi-Fi is 2.4 GHz only (about 33 Mbps measured). 512 MB of memory, so RasBeacon's browser (hotel login pages, speed test) is slower and leans on the swap file. The USB adapter needs a micro-USB OTG cable; the card's drive is called `BOOT`. |
| Raspberry Pi 3B | `rasbeacon-pi3-zero2w` | Untested (best effort) | Same image as the Zero 2 W. 1 GB of memory. Ethernet is 100 Mbit/s (smsc95xx, on USB). Built-in Wi-Fi is 2.4 GHz only (CYW43430). Ethernet and the USB Wi-Fi adapter share one USB 2.0 connection, so expect about 100 Mbit/s with both busy. |
| Raspberry Pi 3B+ | `rasbeacon-pi3-zero2w` | Untested (best effort) | Same image as the Zero 2 W. 1 GB of memory. Gigabit Ethernet on USB 2.0 (lan78xx, about 300 Mbit/s in practice). Built-in Wi-Fi is 2.4 and 5 GHz (CYW43455). Ethernet and the USB adapter share the USB connection (about 200 to 300 Mbit/s with both busy). Needs a 5 V / 2.5 A supply. |
| Raspberry Pi 3A+ | — | Not supported | Would likely start the same image, but 512 MB and no Ethernet with one USB port; not built for or tested. |
| Raspberry Pi Zero W / Zero (original) | — | Not supported | 32-bit, single-core ARMv6; RasBeacon images are 64-bit. |

## USB Wi-Fi adapters (hotspot radio)

**Gate for "Tested":** at least 10 devices associated with WPA2/WPA3 for 30
minutes with no drops, plus power draw at idle and under `iperf3` load. See
[testing/m1.md](testing/m1.md).

The guest network needs an adapter whose driver can run two access points
at once. RasBeacon reads this from the driver (`iw phy` "valid interface
combinations") and the Guests page says when it can't. MediaTek (mt76)
drivers generally can; please report what the Realtek adapters show.

| Adapter | Chip (Linux driver) | USB ID | Status | Max clients | Throughput | Power | Notes |
|---|---|---|---|---|---|---|---|
| Alfa AWUS036ACM | MT7612U (mt76x2u) | 0e8d:7612 | Untested (reference) | Driver table 128; 10 unverified | 400+ Mb/s AP on 5 GHz/80 MHz (Pi 4B, iperf3, third-party) | About 380 mA heavy load (third-party); idle unmeasured | WPA3-SAE supported |
| TP-Link Archer T3U | RTL8812BU (rtw88_8822bu) | 2357:012d | Untested, **at risk** | Driver table 32; 10 unverified | — | — | Reported AP-mode drops ("failed to download beacon") on older kernels. Pi kernel 6.18. |
| TP-Link Archer T3U Plus | RTL8812BU (rtw88_8822bu) | 2357:0138 | Untested, at risk | as above | — | — | |
| TP-Link Archer T4U v3 | RTL8812BU (rtw88_8822bu) | 2357:0115 | Untested, at risk | as above | — | — | |
| TP-Link Archer T4U v1/v2 | RTL8812AU (rtw88_8812au) | 2357:0101, 0103, 010d | Untested | Driver table 32 | — | — | In-kernel driver since Linux 6.14 |
| MT7921AU adapters (e.g. Alfa AWUS036AXML) | MT7921AU (mt7921u) | varies | Untested (fallback) | Driver table 15 | — | Higher (USB 3, Wi-Fi 6) | AP mode since Linux 5.19 |
| Built-in Wi-Fi (any Pi) as the hotspot | brcmfmac | — | **Known-limited** | About 8 (third-party reports) | — | — | Used only as a fallback; RasBeacon warns on the status page |

## Power banks

| Model | Capacity | Status | Auto-off with RasBeacon idle? | Pass-through charging? | Measured runtime |
|---|---|---|---|---|---|
| *(add yours)* | | | | | |

**What to note:**
- Does the bank switch off when the load is low? This is common below about
  50 to 200 mA.
- Does the Pi reboot when you plug the bank into a charger while it's
  running? (Pass-through dropouts.)
- Does the bank have an "always on" or "low current" mode?
