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
[testing/full.md](testing/full.md) sections 15 (Robustness: long run) and 17
(Measurements).

The guest network needs an adapter whose driver can run two access points
at once. RasBeacon reads this from the driver (`iw phy` "valid interface
combinations") and the Guests page says when it can't. MediaTek (mt76)
drivers generally can; please report what the Realtek adapters show.

| Adapter | Chip (Linux driver) | USB ID | Status | Max clients | Throughput | Power | Notes |
|---|---|---|---|---|---|---|---|
| Alfa AWUS036ACM | MT7612U (mt76x2u) | 0e8d:7612 | Untested (reference) | Driver table 128; 10 unverified | 400+ Mb/s AP on 5 GHz/80 MHz (Pi 4B, iperf3, third-party) | About 380 mA heavy load (third-party); idle unmeasured | WPA3-SAE supported |
| TP-Link Archer T3U | RTL8812BU (rtw88_8822bu) | 2357:012d | Tested, **stalls** (see notes) | Driver table 32; 10 unverified | — | — | On the owner's Pi 4 (kernel 6.18), the hotspot stops sending data to its devices after a while: they stay connected but get no internet, with WPA2 and WPA3 alike. Restarting the hotspot fixes it, and RasBeacon now does that by itself within about two minutes, recording each stall in the diagnostics report ("hotspot stalls"). Reported AP-mode drops ("failed to download beacon") on older kernels. |
| TP-Link Archer T3U Plus | RTL8812BU (rtw88_8822bu) | 2357:0138 | Untested, at risk | as above | — | — | |
| TP-Link Archer T4U v3 | RTL8812BU (rtw88_8822bu) | 2357:0115 | Untested, at risk | as above | — | — | |
| TP-Link Archer T4U v1/v2 | RTL8812AU (rtw88_8812au) | 2357:0101, 0103, 010d | Untested | Driver table 32 | — | — | In-kernel driver since Linux 6.14 |
| MT7921AU adapters (e.g. Alfa AWUS036AXML) | MT7921AU (mt7921u) | varies | Untested (fallback) | Driver table 15 | — | Higher (USB 3, Wi-Fi 6) | AP mode since Linux 5.19 |
| Built-in Wi-Fi (any Pi) as the hotspot | brcmfmac | — | **Known-limited** | About 8 (third-party reports) | — | — | Used only as a fallback; RasBeacon warns on the status page |

## Phones and USB modems (internet over USB)

A phone sharing its connection over USB, or a "HiLink" modem (one that
works like a small router with its own DHCP server), carries the internet
when neither Ethernet nor Wi-Fi has it. Modems that need a modem manager
(QMI or MBIM: most M.2 and many 5G modems) work through ModemManager: the
Internet page's *Mobile modem* takes the carrier's APN and the SIM's PIN
(tried once: a refused PIN is never sent again, so it can't block the
SIM). A Pi 4's USB
ports give 1.2 A in total: a modem that draws more needs a powered USB hub.

| Device | Driver | Status | Notes |
|---|---|---|---|
| Android phones (USB tethering) | rndis_host, cdc_ncm | Untested (reference) | |
| iPhone (Personal Hotspot over USB) | ipheth + usbmuxd | Untested | Tap *Trust* the first time; the pairing is kept on the settings partition |
| Huawei E3372h (HiLink) | cdc_ether | Untested | |
| ZTE / Alcatel HiLink sticks | cdc_ether, cdc_ncm | Untested | |
| Quectel EM12-G / RM520N-GL (M.2 in a USB case) | qmi_wwan + ModemManager | Untested | Needs a powered USB hub on a Pi 4 |
| Sierra Wireless EM7455 / EM9191 | qmi_wwan or cdc_mbim + ModemManager | Untested | |

## Wi-Fi 6, 6E and 7 on the hotspot

RasBeacon runs the best standard the hotspot's adapter can (it reads what
the driver allows: `iw phy`, "HE Iftypes: … AP"), and the Hotspot page only
offers those. Older devices still join a Wi-Fi 6 network.

The hotspot also claims exactly the 802.11n/ac/ax features the driver
reports for the band (short guard intervals, LDPC, STBC, beamforming,
larger frames), so devices use the fastest rates both sides support. If
hostapd still refuses them, RasBeacon starts the hotspot without them, then
as Wi-Fi 4, and the Status page says so.

| Adapter | Best hotspot standard |
|---|---|
| MT7921AU adapters (e.g. Alfa AWUS036AXML) | Wi-Fi 6 on 2.4 and 5 GHz |
| Alfa AWUS036ACM (MT7612U) | Wi-Fi 5 on 5 GHz, Wi-Fi 4 on 2.4 GHz |
| TP-Link Archer T3U/T4U (rtw88) | Wi-Fi 5 on 5 GHz, Wi-Fi 4 on 2.4 GHz |
| Built-in Wi-Fi (Pi 4/5, 3B+) | Wi-Fi 5 on 5 GHz; Wi-Fi 4 on 2.4 GHz (Zero 2 W and 3B: 2.4 GHz only) |

**Wi-Fi 6E (6 GHz)** is offered when two things allow it:

- the adapter: its driver must list Wi-Fi 6 in AP mode on the 6 GHz band,
  with its channels (`iw phy`, "Band 4"). The
  System page shows "Wi-Fi 6E" next to such an adapter. Whether the
  MT7921AU driver on the Pi's kernel allows it isn't confirmed yet:
  report what you find.
- the country: Linux's regulatory database lets a radio start a 6 GHz
  network in much of Europe, the UK, Japan and Canada (in Europe and the UK
  only up to 6425 MHz), but **not in the US**, where the whole band is
  marked "no IR" until Linux supports the FCC's rules for it. RasBeacon
  offers 6 GHz only where it is allowed.
  If the Wi-Fi network RasBeacon joins for the internet announces a
  country, Linux allows only what both countries allow, so a hotel
  announcing "US" rules out a 6 GHz hotspot even when RasBeacon is set to
  another country.

A 6 GHz hotspot is WPA3 only (with hash-to-element), runs Wi-Fi 6 on a
"preferred scanning channel" (5, 21, 37 …) at 80 MHz where it fits, and is
seen only by Wi-Fi 6E devices (from about 2021). The guest network on the
same radio follows; a guest radio of its own stays on 2.4 GHz.

**Wi-Fi 7 isn't offered for the hotspot.** It needs hostapd 2.11 or newer
(Debian 13 ships 2.10) and an adapter whose Linux driver runs a Wi-Fi 7
access point, which no USB adapter does on the Pi's kernel yet. The System
page shows "Wi-Fi 7" for an adapter whose driver lists it.

## Wi-Fi standards on the uplink

The uplink joins whatever the adapter and the network both support; the
Internet page shows the standard in use (from the rates: Wi-Fi 4 to 7, and
6E on 6 GHz), the channel width and the link speed both ways, and the scan
list shows what each network offers.

- **6 GHz networks (Wi-Fi 6E)** need an uplink adapter with 6 GHz (USB;
  the Pi's built-in Wi-Fi is 2.4 and 5 GHz only), and are WPA3 only.
  RasBeacon allows both WPA3 password methods (`sae_pwe=2`), which 6 GHz
  needs.
- **Wi-Fi 7 networks** are joined with one link by a Wi-Fi 7 adapter (the
  driver handles it). Using several links at once (MLO) needs
  wpa_supplicant 2.11, not in Debian 13.

## USB Ethernet adapters (extra wired ports for devices)

A USB Ethernet adapter becomes a wired port of the hotspot, so the built-in
Ethernet port can carry the internet while a computer, a console or a
switch still connects by cable (on the Pi Zero 2 W, which has no Ethernet
port, it is the only wired port). Like the Ethernet client port, it joins
the hotspot only when no DHCP server answers on it. Any adapter with an
in-kernel driver should work; phones and modems (drivers `rndis_host`,
`ipheth`, `qmi_wwan`, `cdc_mbim`, `huawei_cdc_ncm`) are never used as ports
for devices.

| Adapter | Chip (Linux driver) | Status | Notes |
|---|---|---|---|
| Generic USB 3 gigabit adapters | RTL8153 (r8152) | Untested (reference) | The most common chip |
| Generic USB 3 gigabit adapters | AX88179 (ax88179_178a) | Untested | |
| Generic USB 2 100 Mb/s adapters | AX88772 (asix) | Untested | |

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
