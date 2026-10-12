# ADR 0001: Primary platform for v1

- Status: **Accepted** (2026-09-30)
- Date: 2026-09-30, amended 2026-10-12 (power)

Section 0 is the decision. Sections 1 to 8 are the analysis behind it,
starting from an initial proposal; where they differ, section 0 wins.

## 0. Decision

1. Platform and base OS: Raspberry Pi, with Raspberry Pi OS Lite
   (trixie, arm64) built with `rpi-image-gen`.
2. **Reference hardware is a Raspberry Pi 4 or newer (Pi 4B / Pi 5) plus one
   MT7612U adapter** (option D), not the Zero 2 W. Consequences:
   - The onboard CYW43455 (2.4/5 GHz) is the Wi-Fi uplink. Built-in Gigabit
     Ethernet is also available as an uplink.
   - The config ceiling is well above 40 Mbps (section 5).
   - **Power:** there is no battery requirement (amended 2026-10-12).
     RasBeacon runs from a USB-C power supply or a power bank. On a bank, a
     Pi 4B lasts about 7 h on 10,000 mAh or 14 h on 20,000 mAh (estimates).
   - Measured Pi 4B idle figures vary by source, from about 1.4 W (275 mA) to
     2.9 W. Raspberry Pi's "typical active" figures are 600 mA (Pi 4B) and
     800 mA (Pi 5). We will measure our own in milestone 1.
   - **Pi 5 quirk:** on any supply that isn't 5 V / 5 A (which covers nearly
     every power bank), the Pi 5 limits all USB ports to 600 mA combined. An
     MT7612U under heavy load draws about 380 mA, so it fits, but a second
     adapter or a USB Ethernet dongle may not.
   - The Zero 2 W and 3A+ remain untested/community entries in the
     compatibility table; the image targets arm64 boards and doesn't block them.
3. **The hotspot defaults to 2.4 GHz**, with a setting in the web UI to switch
   it to 5 GHz. This also removes the Apple Watch question: every Apple Watch
   joins 2.4 GHz.


## 1. Initial proposal (superseded by section 0)

**Platform: Raspberry Pi running Linux, with a USB Wi-Fi adapter on a MediaTek
MT7612U chip as the hotspot radio.** The OS is a read-only image built from
Raspberry Pi OS Lite (Debian 13 "trixie", arm64) with `rpi-image-gen`. The
hotspot, NAT, WireGuard and kill-switch stack is our own code. We learn from
esp32_nat_router, RaspAP and Travelmate but do not fork them.

**Reference configuration (v1): Raspberry Pi Zero 2 W + one MT7612U adapter.**
- The MT7612U is the hotspot, on 5 GHz by default.
- The onboard radio is the uplink.
- It runs on a 10,000 mAh power bank (20,000 mAh recommended).

The same image also supports two alternatives:
- **B:** Zero 2 W plus a second MT7612U as the uplink.
- **C:** Pi 3A+ plus one MT7612U, where the 3A+'s onboard 5 GHz radio is the uplink.

The image assigns roles automatically based on which radios it finds.

**The ESP32 is rejected.** It fails two pass/fail requirements: 10 associated
clients, and about 40 Mbps through NAT and WireGuard. Section 6 covers whether
it could be a later port.

**Things dropped or degraded in the reference config (details in section 5):**
1. **Uplink throughput.** The Zero 2 W's onboard radio is 2.4 GHz only. The
   best measured TCP rate is about 33 to 35 Mbps, so reference config A tops out
   around 30 Mbps through WireGuard, below the 40 Mbps target. Configs B and C
   meet the target but draw more power. This is the battery-versus-throughput
   tradeoff that section 0 settled in favor of throughput.
2. **WPA3-only uplinks on the onboard radio.** Broadcom FullMAC (brcmfmac)
   support for WPA3 and protected management frames is inconsistent across
   firmware. WPA2-Enterprise, WPA2-Personal and open networks are fine. A
   WPA3-only uplink needs config B, where a mac80211 USB adapter handles the
   uplink.
3. **10-client verification.** The MT7612U driver has a 128-entry station table.
   No published test of 10 clients associated with WPA2/WPA3 on this chip was
   found. Passing that test is therefore the first hardware gate in
   milestone 1, not an assumption. If it fails, the fallback is an MT7921AU
   adapter, whose driver table allows 15 stations.

## 2. Options considered

| ID | Configuration | Hotspot radio | Uplink radio |
|---|---|---|---|
| A | **Pi Zero 2 W + 1× MT7612U** (reference) | MT7612U, 5 GHz 2x2 (2.4 GHz optional) | Onboard BCM43436, 2.4 GHz 1x1 |
| B | Pi Zero 2 W + 2× MT7612U (small USB hub) | MT7612U #1 | MT7612U #2, 5/2.4 GHz 2x2; onboard off |
| C | Pi 3A+ + 1× MT7612U | MT7612U | Onboard CYW43455, 5/2.4 GHz 1x1 |
| D | Pi 4B + 1× MT7612U | MT7612U | Built-in Ethernet or onboard CYW43455 |
| E | Pi (any) using only the onboard radio (AP and STA on one chip) | Onboard | Onboard, same channel |
| F | ESP32-S3 (or C5 for 5 GHz) running esp32_nat_router or our own firmware | Single radio, AP and STA share a channel | Same radio |

**Existing projects:**
- **martin-ger/esp32_nat_router** (ESP32 firmware; WireGuard component is BSD-3): has NAT,
  WPA2-Enterprise (PEAP/TTLS/TLS), WireGuard, a web installer and OTA updates.
  - Its README says the client limit is **8 (5 on the C3)**, set by RAM.
  - It reports **5 to 15 Mbps**.
  - It validates the server certificate against the built-in CA bundle and has
    an option to skip the certificate time check. There is no documented upload
    of a custom CA or server-name check, although ESP-IDF itself has
    `esp_eap_client_set_ca_cert` and `esp_eap_client_set_domain_name`.
  - The web UI has no TLS.
  - Verdict: learn from it (it proves the feature set fits on an MCU), don't build on it.
- **RaspAP** (GPL-3.0, PHP): has AP+STA, WireGuard, OpenVPN and prebuilt Pi OS
  images. It ships with shared default credentials (`admin`/`secret`, Wi-Fi
  password `ChangeMe`), which violates our safe-defaults goal, and has no
  documented fail-closed kill switch. Verdict: learn only. Forking would bring
  its web stack and defaults along.
- **OpenWrt Travelmate:** uplink manager with captive-portal detection (a
  "heartbeat"), auto-login scripts, enterprise uplinks and WireGuard/OpenVPN
  reconnect. It has no kill switch. Verdict: learn from its portal-detection
  and uplink-switching behavior. It is OpenWrt-specific.

## 3. Evidence behind the key numbers

| Item | Value | Source / status |
|---|---|---|
| Pi onboard (Broadcom FullMAC) AP client limit | About 8 clients (3B+ on OpenWrt); 9 to 12 reported on a 4B; limited by on-chip RAM | OpenWrt forum, World Possible forum, raspberrypi/linux#3010. **Does not reliably meet the 10-client requirement.** |
| MT7612U AP (mt76x2u) | 128 WCID station table; SAE (WPA3) supported; MFP supported by mt76; "400+ Mb/s" iperf3 at 5 GHz/80 MHz on a Pi 4B; "max about 380 mA under heavy load" | Linux `mt76x02.h`, `mt76/mac80211.c`; morrownr/USB-WiFi (AWUS036ACM review, TEROW ROW02CD listing). **10 associated clients unverified.** Idle current unmeasured. |
| MT7921AU AP (mt7921u) | Station table of 20 − 1 − 4 = **15**; AP mode since kernel 5.19; draws more power (USB 3, Wi-Fi 6) | Linux `mt792x.h`; morrownr. Fallback only. |
| Zero 2 W power | 120 mA idle (Lite); 75.5 mA with HDMI/KMS off; +11 mA with Wi-Fi associated; **+187 mA average during Wi-Fi iperf**; 635 mA peak under 4-core stress, 449 mA with cores capped at 600 MHz | CNX Software measurements (2021) |
| Pi 3A+ power | About 240 mA idle | raspi.tv measurements |
| Pi "typical active" current | Zero 2 W 350 mA, 3A+ 350 mA, 4B 600 mA | Raspberry Pi docs (power-supplies page) |
| Zero 2 W onboard Wi-Fi throughput | 32 to 35 Mbps iperf3, 2.4 GHz, 2 m line of sight | bret.dk, Hackster reviews |
| CYW43455 (3A+/3B+) 5 GHz | About 102 Mbps iperf | Raspberry Pi 3B+ launch data |
| WireGuard crypto (network namespaces, not forwarding) | Zero 2 W 443 Mbps; Pi 3B 522 Mbps; Pi 4B 881 Mbps | OpenWrt "WireGuard comparison DB" |
| ESP32 softAP | `ESP_WIFI_MAX_CONN_NUM` = 15; shares hardware key slots with ESP-NOW; in AP+STA mode "the home channel of AP and station must be the same" | ESP-IDF headers, Kconfig and Wi-Fi guide |
| Power-bank auto-off | Typically below about 50 to 200 mA | Vendor and maker sources; varies by bank. **Test your bank.** |

**CPU is not the bottleneck on any Pi option:** the WireGuard crypto ceiling is
10 times the target or more. The limit is radio airtime and the USB 2.0 bus.

## 4. Scoring (Full / Degraded / Impossible)

Pi columns score config **A**. Where B or C changes the result, it's noted in the cell.

| Requirement | Raspberry Pi (config A) | ESP32 (S3 / C5) |
|---|---|---|
| EAP-TTLS/PAP with custom CA and RADIUS server-name check | **Full.** wpa_supplicant `ca_cert` plus `domain_suffix_match`/`domain_match`. PEAP and EAP-TLS also full. | **Degraded.** The IDF API can do it, but it's not in the reference firmware, and the whole EAP stack competes for RAM with NAT and WireGuard. |
| WPA3-Enterprise uplink | **Degraded** on onboard brcmfmac (PMF/WPA3 depends on firmware; WPA2-Enterprise fallback). **Full** in config B (mac80211 plus wpa_supplicant). | **Degraded:** WPA3-Enterprise is supported by IDF but untested together with NAT and WireGuard. |
| Hotspot and uplink at the same time | **Full:** separate radios on separate bands (hotspot 5 GHz, uplink 2.4 GHz), no channel coupling. | **Degraded:** one radio, so the AP must follow the uplink's channel; clients are moved with a channel-switch announcement; airtime is halved. |
| 10 associated clients with WPA2/WPA3 | **Expected full, unverified.** Driver table is 128; must pass the milestone-1 test. The onboard radio alone (config E) fails at about 8. | **Impossible in practice:** reference firmware caps at 8 because of RAM; the chip's table is 15, but not with WireGuard, EAP and a UI loaded. |
| 40 Mbps through NAT + WireGuard (config ceiling, separate from uplink) | **Degraded: about 30 Mbps**, capped by the 2.4 GHz onboard uplink. B and C: **full** (C about 90 Mbps estimated; B above 40 Mbps estimated, bounded by shared USB 2.0). | **Impossible:** 5 to 15 Mbps NAT before WireGuard. |
| Kill switch (fail-closed, no leaks during boot or changes) | **Full:** nftables policy set atomically before any interface comes up, plus fwmark policy routing. | **Degraded:** needs custom lwIP netif hooks; no firewall framework; hard to prove leak-free. |
| Captive portal with kill switch ON | **Full:** all three approaches possible (per-client temporary pass-through, UI proxy, remote browser). The browser option is heavy on 512 MB. | **Degraded:** time-limited pass-through only. |
| IPv6 handling / DNS / MSS clamp | **Full** (nftables, dnsmasq) | **Degraded** (lwIP IPv6 forwarding is limited; block-only) |
| Status, logs, diagnostics export | **Full** | **Degraded** (small RAM log ring) |
| Battery, 12 h on a common bank | **Full on 10,000 mAh at a mixed duty cycle (~13 h estimated, thin margin); comfortable on 20,000 mAh.** | **Full** (~0.5 to 0.8 W estimated), but low draw risks the bank's auto-off |
| Cost and availability | About $55 to $75 (Zero 2 W $15, MT7612U adapter $20 to $45, microSD, OTG adapter) | About $10 to $15 |
| First-time flashing for a non-expert | **Full:** Raspberry Pi Imager or balenaEtcher "use custom image" | **Full:** WebSerial installer (Chrome/Edge only) |
| Update path | **Full:** A/B root partitions using the Pi firmware's `tryboot` one-shot boot, with automatic rollback; upload through the web UI | **Full** (OTA) |
| Survive abrupt power loss | **Full:** read-only root plus small journaled config partition with atomic writes | **Full** (NVS is power-safe) |
| Guest isolation, DHCP option 43, mDNS | **Full** | **Degraded** |
| Contributor accessibility and CI | **Full:** Linux, nftables, Python/Go; network-namespace tests run on stock GitHub runners | **Degraded:** ESP-IDF C; network logic hard to test on the host |
| TLS on admin UI | Possible (self-signed, per-device) | Reference firmware says "TLS is not implemented" (RAM) |

## 5. Configurations: throughput and power

Throughput ceilings are **per configuration** and assume a fast uplink. The
real uplink is often slower. All power figures are **estimates** built from the
measurements above; milestone 1 measures real values with a USB power
meter.

| Config | Config ceiling through WireGuard | Idle, clients associated | Under load | Mixed use* | 5,000 mAh | 10,000 mAh | 20,000 mAh |
|---|---|---|---|---|---|---|---|
| **A** Zero 2 W + MT7612U | about 30 Mbps (2.4 GHz uplink) | 1.6 to 1.9 W | 3.5 to 4.0 W | ~2.3 W | ~6.5 h | **~13 h** | ~26 h |
| B Zero 2 W + 2× MT7612U | above 40 Mbps (est. 80 to 150, USB-bound) | 2.1 to 2.6 W | 4.5 to 5 W | ~2.9 W | ~5 h | ~10 h | ~20 h |
| C Pi 3A+ + MT7612U | about 90 Mbps (5 GHz uplink) | 2.1 to 2.4 W | 4.3 to 4.8 W | ~2.8 W | ~5.3 h | ~10.5 h | ~21 h |
| D Pi 4B + MT7612U | above 200 Mbps; Ethernet uplink | 3.5 to 4 W | 6 to 7 W | ~4.3 W | ~3.4 h | ~7 h | ~14 h |
| F ESP32-S3 | 5 to 15 Mbps before WireGuard | ~0.5 W | ~0.8 W | ~0.6 W | ~25 h | ~50 h | — |

\* Mixed use means 75% of the time idle with clients connected and 25% under
load. Usable energy is estimated as mAh × 3.7 V × 0.8, which covers conversion
to 5 V and wear.

**Tradeoff:** A is the only Pi configuration that reaches 12 hours on a
10,000 mAh bank, and it gives up about a quarter of the throughput target.
B and C reach the throughput target, need about a 15,000 to 20,000 mAh bank
for 12 hours, and B also restores full WPA3-Enterprise uplink support. The
initial proposal favored A, with B as a plug-in upgrade. The decision
(section 0) chose D for throughput instead and later dropped the battery
target. A 20,000 mAh bank (about 74 Wh) is within airline carry-on limits.

**Power-saving measures, applied by default:**
- Disable HDMI/KMS (measured: 120 mA to 75.5 mA on the Zero 2 W).
- Disable Bluetooth and the activity LED.
- Use the `schedutil`/`ondemand` CPU governor with a maximum-frequency cap
  (measured: stress current 635 mA to 449 mA at 600 MHz).
- Turn the onboard radio off when it isn't the uplink (config B).
- Enable station power-save on the uplink.
- Offer 20/40/80 MHz width for the hotspot in the UI.
- No USB autosuspend on the hotspot adapter, because an AP can't sleep.

**Power-bank quirks to document:**
- Auto-off below about 50 to 200 mA. Config A idles around 330 mA, which is
  above most thresholds.
- Pass-through charging often drops the output briefly, which reboots the
  device. Power-loss safety covers this.
- Prefer banks with an "always-on" or low-current mode.
- Use a short, thick cable.

## 6. Decision and justification

1. **Raspberry Pi over ESP32.** ESP32 fails the two pass/fail load requirements
   (10 clients; 40 Mbps with WireGuard) by a wide margin: the reference firmware
   manages 8 clients and 5 to 15 Mbps before WireGuard. It also forces the
   hotspot onto the uplink's channel and makes a provably leak-free kill switch
   and IPv6 handling much harder. Its real advantages are power, price and a
   polished web flasher; the Pi needs a power supply or a larger power bank instead.
   **ESP32 port:** not planned. A "lite" ESP32 build (hotspot + uplink + NAT,
   8 clients or fewer, no kill-switch guarantee) could be a separate future
   project that reuses the web UI's JSON API. It would not share the network
   code.
2. **Two radios, with a MediaTek mac80211 adapter as the hotspot.** The Pi's
   onboard Broadcom chip can't reliably hold 10 clients in AP mode, but it is a
   fine 2.4 GHz uplink. mt76 is the best-supported in-kernel USB AP driver:
   in-kernel since 4.19 and recommended by the main Linux USB Wi-Fi reference
   (morrownr/USB-WiFi). It has no out-of-tree drivers to build, which keeps
   "every feature in prebuilt releases" realistic.
3. **Base OS: Raspberry Pi OS Lite (trixie, arm64), built with
   `rpi-image-gen` (BSD-3-Clause), made read-only with A/B updates via
   `tryboot`.**

   | OS | For | Against |
   |---|---|---|
   | **Pi OS Lite (chosen)** | First-class kernel, firmware and onboard-Wi-Fi support. Redistributable Broadcom firmware is already packaged. Debian security fixes. Official image tool. Most familiar to contributors (systemd, nftables, hostapd, wpa_supplicant). | Bigger image, slower boot. We must build read-only root and A/B updates ourselves. `rpi-image-gen` officially supports only arm64 hosts; GitHub has arm64 runners (x86 with QEMU emulation also works, more slowly). |
   | OpenWrt 25.12 (bcm2710) | Router-native (firewall4, netifd), small, squashfs plus overlay, sysupgrade, excellent mt76 support, Image Builder needs no compiling | Pi is a second-class target. **Pi Wi-Fi firmware isn't shipped because of licensing uncertainty**, which we'd inherit. LuCI and ucode are niche for contributors. Our own UI would still be needed. |
   | Buildroot/Yocto | Smallest, fastest boot, read-only by design | Slow CI builds. We would own every CVE and every package. Hardest for contributors. |

4. **Build on / fork / learn:** start fresh on Pi OS. Learn from:
   - Travelmate: portal heartbeat and uplink switching.
   - esp32_nat_router: feature scope and MSS/PMTU handling.
   - RaspAP: what not to ship by default.

   No code is copied from any of them, so their licenses (GPL-3.0 for RaspAP)
   don't constrain ours. RasBeacon is Apache-2.0 (see the design doc).

## 7. Reference hardware (first version of the compatibility table)

| Part | Role | Status | Verified numbers so far |
|---|---|---|---|
| Raspberry Pi Zero 2 W | Board | Planned reference, untested by us | Power as in section 3; onboard 2.4 GHz about 33 Mbps; WireGuard crypto 443 Mbps |
| Raspberry Pi 3A+ | Board (config C) | Untested | 240 mA idle; 5 GHz about 102 Mbps (3B+ figure) |
| Raspberry Pi 4B | Board (config D) | Untested | WireGuard crypto 881 Mbps |
| MT7612U (e.g. Alfa AWUS036ACM, TEROW ROW02CD) | Hotspot / uplink | Planned reference, untested by us | About 380 mA under heavy load; 400+ Mb/s AP iperf3 (Pi 4B); SAE yes; clients: **to verify (≥10)**; idle: **to measure** |
| MT7921AU (e.g. Alfa AWUS036AXML) | Fallback hotspot | Untested | Driver station table 15; AP since kernel 5.19 |
| Onboard Broadcom (any Pi) as hotspot | — | **Known-limited** | About 8 clients (OpenWrt, 3B+) |
| Power bank | — | — | Document the auto-off threshold and pass-through behavior per model |

Adapters using out-of-tree Realtek drivers are excluded from the reference list.

## 8. Rutgers-specific findings so far

- **CA certificates: fetched and inspected.**
  - USERTrust RSA Certification Authority. Self-signed; valid 2010-02-01 to
    2038-01-18. SHA-256 fingerprint
    `E7:93:C9:B0:2F:D8:AA:13:E2:1C:31:22:8A:CC:B0:81:19:64:3B:74:9C:89:89:64:B1:74:6D:46:C3:D4:CB:D2`.
    This **matches** the Mozilla/Debian root store copy.
  - Rutgers University Device Root CA. Self-signed; valid 2024-06-07 to
    2044-06-07. SHA-256 fingerprint
    `CF:5A:A5:BD:8E:2A:B9:9C:B3:D9:E7:E7:0A:BA:2C:B1:70:74:F3:04:18:76:4D:C2:C9:45:06:FA:0C:6F:BF:09`.
    There is **no independent second source** for this one: compare it with
    the profile Rutgers' own setup installs on a Mac or iPhone.
- **The manual-installation URL now redirects** to a ServiceNow article
  (`ithelp.rutgers.edu … KB0012502`) that renders with JavaScript, so the
  WPA2/WPA3 wording and the `*.ruw.rutgers.edu` server name couldn't be
  re-read from it; the values used come from Rutgers' earlier instructions.
- **WPA2 vs WPA3-Enterprise:** only observable on site. From milestone 3, the
  device logs the advertised security settings of the `eduroam` access
  points it scans (AKM suites, PMF capable/required), and hardware testing
  settles it.

## Sources

- esp32_nat_router README and wiki: https://github.com/martin-ger/esp32_nat_router
- ESP-IDF: `esp_wifi_types_native.h`, `esp_wifi/Kconfig`, Wi-Fi driver overview, `esp_eap_client.h` (github.com/espressif/esp-idf, master)
- Pi onboard AP limit: https://forum.openwrt.org/t/remove-limit-of-8-devices-on-wifi-network-on-raspberry-pi/128734 · https://community.worldpossible.org/t/raspberry-pi-max-wifi-clients-bug/2050 · https://github.com/raspberrypi/linux/issues/3010
- USB adapters: https://github.com/morrownr/USB-WiFi (USB_WiFi_Chipsets.md; in-kernel adapters list; iw_list/TEROW_ROW02CD.txt)
- Linux mt76: `drivers/net/wireless/mediatek/mt76/{mt76x02.h,mt792x.h,mac80211.c}`
- Pi power: https://github.com/raspberrypi/documentation (power-supplies.adoc) · https://www.cnx-software.com/2021/12/09/raspberry-pi-zero-2-w-power-consumption/ · https://raspi.tv/2018/how-much-power-does-raspberry-pi-3a-plus-use
- Wi-Fi throughput: https://bret.dk/raspberry-pi-zero-2-w-benchmarks-comparison/ · https://www.hackster.io/news/raspberry-pi-zero-2-w-review-hands-on-with-the-fastest-zero-ever-b85b155905a5 · https://www.raspberrypi.com/news/raspberry-pi-3-model-bplus-sale-now-35/
- WireGuard: https://forum.openwrt.org/t/a-wireguard-comparison-db/187586
- brcmfmac WPA3: https://lists.infradead.org/pipermail/hostap/2024-June/042778.html · https://github.com/RPi-Distro/firmware-nonfree/issues/41
- OpenWrt on Zero 2 W and firmware licensing: https://forum.openwrt.org/t/raspberry-pi-zero-2-w-and-openwrt-23-05-x/178340 · https://openwrt.org/releases/25.12/notes-25.12.0-rc1
- Travelmate: https://openwrt.org/docs/guide-user/network/wifi/wifiextenders/travelmate
- RaspAP: https://github.com/RaspAP/raspap-webgui
- rpi-image-gen: https://github.com/raspberrypi/rpi-image-gen
- Power-bank auto-off: https://0110.be/posts/Powering_low_current_electronics_projects_with_power_banks
