# Design: open-source portable Wi-Fi travel router

- Status: **Proposed, revision 2**. Owner answers to Q1–Q12 are folded in; Q3, Q5, Q7 and Q12 are still open (section 13).
- Date: 2026-09-30
- Depends on: [ADR 0001](decisions/0001-platform.md), which is accepted
- Working name: `travelrouter`. Daemons are `trd` (privileged agent) and
  `trweb` (web UI). The final name is still open (Q1). The license is **Apache-2.0**.

Where this doc says "verified", the evidence is cited in ADR 0001 or here.
"To verify" marks items that can only be settled on your hardware or on
eduroam, and says which milestone checks them.

---

## 1. Hardware baseline

| Part | Role | Notes |
|---|---|---|
| Raspberry Pi 4B (2 GB or more) or Pi 5 | Board (reference) | arm64. Pi 5 has an RTC (needs its optional battery to keep time while off). |
| Raspberry Pi Zero 2 W | Board (**supported**, lower throughput) | 512 MB RAM. Onboard radio is 2.4 GHz only (about 33 Mbps measured), so the Wi-Fi uplink ceiling is about 30 Mbps through the tunnel. No Ethernet. The adapter plugs in through a micro-USB OTG adapter. Best battery life (ADR §5, config A). |
| Onboard CYW43455 (Pi 4/5) or BCM43436 (Zero 2 W), brcmfmac | **Wi-Fi uplink** | WPA2-Personal/Enterprise, open. WPA3 depends on firmware (M3 checks this). |
| Built-in Gigabit Ethernet (Pi 4/5) | **Wired uplink** by default (Q12) | |
| USB adapter with MT7612U (mt76x2u), e.g. Alfa AWUS036ACM | **Hotspot** (reference): 2.4 GHz default, 5 GHz optional | 128-entry station table. SAE and MFP supported. 2 AP interfaces (main + guest). |
| TP-Link "AC1300" USB adapters (Realtek, rtw88 driver) | **Hotspot** (supported, **at risk**) | TP-Link sells several AC1300 models with different chips: **Archer T3U, T3U Plus and T4U v3 use RTL8812BU**; **Archer T4U v1/v2 use RTL8812AU**. Both drivers are in the Raspberry Pi kernel (6.18, `CONFIG_RTW88_8822BU` and `CONFIG_RTW88_8812AU`), with a 32-entry station table and MFP (needed for WPA3). **Known risk:** users have reported the RTL8812BU dropping clients in AP mode ("failed to download beacon", "timed out to flush queue"). The ≥10-client M1 gate runs on this adapter too. Run `lsusb` to identify yours: `2357:012d`, `2357:0138` and `2357:0115` are RTL8812BU; `2357:0101`, `2357:0103` and `2357:010d` are RTL8812AU. |
| USB power bank | Power | About 20,000 mAh for about 12 h on a Pi 4/5 (ADR §0); a Zero 2 W gets about 12 to 14 h from 10,000 mAh (estimate). Each bank's auto-off threshold and pass-through behavior are documented per model. |

Roles are assigned automatically:
- A mac80211 USB adapter that supports AP mode becomes `ap0`.
- The onboard radio becomes `wlan0`.
- Ethernet is `eth0`.

The UI lets you swap roles, for example to use a second USB adapter as the uplink.

The **M1 hardware gate runs on every hotspot adapter we list**. It must hold at
least 10 associated clients with WPA2/WPA3 for 30 minutes with no drops. The
compatibility table records the result.

## 2. Build, image, storage and release

### 2.1 Image build
- **Tool:** `rpi-image-gen` (BSD-3-Clause) builds from Raspberry Pi OS / Debian
  trixie arm64 packages. It compiles nothing except our own Go binaries,
  which are cross-compiled.
- **Image contents:**
  - Kernel and firmware
  - `hostapd`, `wpa_supplicant`, `dnsmasq`, `nftables`, `wireguard-tools`,
    `systemd-networkd`, `systemd-timesyncd`, `iw`, `conntrack`
  - Our two binaries and static web assets

  **Removed:** NetworkManager, desktop/X, Bluetooth services, avahi, and
  cloud-init. **SSH** is installed but **off by default**, with a setting in
  the UI to turn it on (key-only, hotspot side only).
- **CI:** GitHub Actions.
  - `ci.yml` runs on every push and PR: Go vet and tests, golden-file tests,
    network-namespace integration tests, and hwsim tests (section 8).
  - `release.yml` runs on version tags and builds the image. It uses
    `ubuntu-24.04-arm` runners, which are free only for **public** repos. This
    repo stays private until you move the project to a public repo (Q2). Until
    then, `release.yml` can be started by hand, and it falls back to an x86
    runner with QEMU user emulation, which is slower. Nothing in the code or
    workflows hard-codes the repository name, so the move is a plain push.
- **Release artifacts:**
  - `travelrouter-<ver>-rpi-arm64.img.xz`: flash with Raspberry Pi Imager
    ("Use custom") or balenaEtcher.
  - `travelrouter-<ver>.update`: signed update bundle.
  - `SHA256SUMS` and `SHA256SUMS.sig` (Ed25519, minisign-compatible format).
  - GitHub build-provenance attestation.
  - `sources-<ver>.tar`: GPL/LGPL corresponding source (section 10).

  Every feature is in the single image. There are no variants.

### 2.2 Partition layout (GPT; Pi 4/5 bootloader A/B "tryboot")

| # | FS | Size | Contents |
|---|---|---|---|
| 1 | FAT | 64 MB | `autoboot.txt` (`tryboot_a_b=1`), plus the user-visible files `setup.txt` and `factory-reset` (section 5) |
| 2 | FAT | 256 MB | Boot slot A: firmware, kernel, `cmdline.txt` pointing to root A |
| 3 | FAT | 256 MB | Boot slot B |
| 4 | ext4, mounted **read-only** | 1.5 GB | Root A |
| 5 | ext4, mounted read-only | 1.5 GB | Root B |
| 6 | ext4 (`data=journal`, `noatime`) | Rest of the card, grown on first boot | `/data`: config, secrets, saved clock, leases |

- `/var/log`, `/tmp` and `/run` are tmpfs, and journald is `Storage=volatile`.
  Nothing writes to flash except explicit config saves, the clock save and
  updates.
- **Config writes are atomic:** write a temp file, `fsync`, `rename`, then
  `fsync` the directory. On abrupt power loss you get the previous or the new
  config, never a partial one.
- **Update flow, following the Pi firmware's documented A/B procedure:**
  1. Verify the bundle's Ed25519 signature and check that its version is not
     older than the running one. A downgrade needs an explicit confirmation.
  2. Write the inactive boot and root slots, verify their hashes, then run
     `reboot "0 tryboot"`.
  3. The new slot runs a health check: `trd` running, `ap0` beaconing, the UI
     answering locally, all within 180 s. If it passes, `autoboot.txt` is
     swapped to commit the update. If it fails, a normal reboot, which
     automatically returns to the old slot.
- **How updates arrive:** you upload the `.update` file through the UI from a
  phone or laptop. An opt-in "Check GitHub for updates" button fetches the
  releases feed directly and is off by default (no cloud dependency, no
  telemetry). Reflashing the SD card always works; export your config first.
- **Security fix cadence:** CI rebuilds monthly, and within days of a
  high-severity CVE in a shipped package. Rebuilt images pull from the Debian
  and Raspberry Pi security archives.

## 3. Processes and privilege separation

```
hotspot clients (phones, laptops)
        │  HTTP(S) to 10.8.0.1 only
        ▼
trweb   user "trweb", no capabilities, systemd-hardened
        (ProtectSystem=strict, NoNewPrivileges, AF_INET/AF_UNIX only)
        │  typed JSON-RPC over /run/trd/api.sock (0660 root:trweb)
        ▼
trd     root, systemd-hardened; owns /data/secrets; renders configs to /run/trd;
        drives hostapd / wpa_supplicant (control sockets), nft, wg (netlink),
        networkd, dnsmasq
```

- **`trweb`** serves the static UI and a REST/JSON API. It never reads
  secrets: secret fields are write-only and the API only reports
  "set / not set". It handles:
  - Sessions, login rate limiting and CSRF protection.
  - Admin password hashing (argon2id from `golang.org/x/crypto`, our only
    third-party Go dependency).
- **`trd`** is a single event loop and the **only** writer of network state.
  It owns:
  - Validating and rendering configs.
  - The kill-switch/uplink/portal state machine (section 4.5).
  - Health probes, time sync and updates.
- **Language:** Go, stdlib-first. Each daemon is one static binary, uses about
  10 to 20 MB of RAM, cross-compiles trivially, and Go is familiar to
  contributors.
- **Frontend:** plain HTML, CSS and ES modules, no build step and no npm.
  Mobile-first, one screen per task, works offline, and all assets are served
  from the device.

Secrets:
- Stored in `/data/secrets/*.json`, `root:root 0600`.
- Rendered into `/run/trd/*.conf` at 0600.
- Never logged:
  - `wpa_supplicant` and `hostapd` run without `-d`.
  - `trd`'s logger redacts known secret keys.
  - The eduroam identity is shown masked (`N***@rutgers.edu`) in logs and exports.

Release images contain no keys, passwords or per-device material. Everything
per-device is generated on first boot.

## 4. Network design

### 4.1 Interfaces and addressing

| Interface | Managed by | Address |
|---|---|---|
| `ap0` (hotspot) | hostapd + `trd` | `10.8.0.1/24`, plus an IPv6 ULA `/64` (4.4). If it collides with the uplink subnet **or the WireGuard tunnel's addresses**, `trd` falls back to `10.9.0.1/24`, pushes short leases and shows why in the UI. `10.8.0.0/24` is also OpenVPN's default subnet and a common WireGuard default (wg-easy), so this fallback may trigger with your server (section 13). |
| `ap0g` (guest, stretch) | hostapd second BSS | `10.8.8.1/24` |
| `wlan0` (Wi-Fi uplink) | wpa_supplicant (driven by `trd`) + networkd DHCPv4/RA | From uplink DHCP |
| `eth0` (wired uplink) | networkd | From uplink DHCP |
| `wg0` | `trd` via netlink | From the `.conf` `Address` |

- **Hotspot:**
  - `hostapd` runs with `country_code=US` (configurable), `ieee80211d=1`,
    2.4 GHz `hw_mode=g`, HT20 by default (HT40 optional), channel auto.
  - The **5 GHz option** uses VHT80 on **non-DFS channels only** (36 to 48,
    149 to 165 in the US). DFS support in a USB hotspot is unverified.
  - Security is WPA2/WPA3 transition mode: `wpa_key_mgmt=WPA-PSK SAE`,
    `ieee80211w=1`, `sae_require_mfp=1`, CCMP. A "WPA2 only" compatibility
    switch is available.
  - `ap_isolate=0` by default, so AirPlay and Bonjour work between your devices.
- **2.4 GHz coexistence:** when the uplink is also on 2.4 GHz, the two radios
  are centimeters apart. Choosing the same channel (shared airtime) versus a
  non-overlapping one (risk of adjacent-channel desense) will be **decided by
  iperf measurement in M2**. The default until then is same-channel. The
  uplink prefers 5 GHz BSSs when the same SSID is on both bands.
- **DHCP (dnsmasq on `ap0` only):**
  - Leases last 1 h and the pool is `.20` to `.250`. Leases are keyed on client
    identifier or MAC, so rotating private MAC addresses just get new leases.
  - The client count comes from **currently associated stations**
    (`hostapd` STA list), never from lease history.

### 4.2 Uplink management
- **Priority:** Ethernet with a link, then saved Wi-Fi networks in your order,
  scanning for any that are in range. `trd` drives `wpa_supplicant` over its
  control socket, one network at a time, and switches when the current uplink
  loses DHCP or connectivity for 20 s.
- **Enterprise uplinks:** `eap=TTLS` + `phase2="auth=PAP"`, `PEAP` +
  `MSCHAPV2`, or `TLS` with a client cert/key. Fields:
  - `identity` and `anonymous_identity`
  - `ca_cert` (uploaded PEM/DER; multiple CAs concatenated)
  - `domain_suffix_match` or `domain_match` (from the "server name" field;
    `*.ruw.rutgers.edu` maps to `domain_suffix_match=ruw.rutgers.edu`)
  - `ieee80211w=1` (PMF optional). `key_mgmt=WPA-EAP WPA-EAP-SHA256` covers
    WPA2- and WPA3-Enterprise.

  **The UI refuses to save an Enterprise network without a CA certificate.**
  An "I understand the risk" override exists only under an expanded Advanced
  section and shows a persistent warning banner. The default is never "no
  validation".
- **eduroam preset:** SSID `eduroam`, TTLS/PAP, empty fields for identity and
  anonymous identity (hint: `anonymous@<realm>`), CA upload and server name.
  Rutgers is documented as an example in `docs/eduroam.md`, not built into the
  code.
- **Regulatory:** one country setting is applied to cfg80211 (`iw reg set`),
  hostapd and wpa_supplicant.

### 4.3 Routing
Tables and rules, with WireGuard enabled:

```
ip rule  90: fwmark 0x5182 lookup main          # WireGuard's own UDP goes via uplink
ip rule 100: iif ap0  lookup 51820              # all client traffic → tunnel table
ip rule 101: iif ap0g lookup 51820
table 51820: default dev wg0
             unreachable default metric 4096    # KS ON: no fall-through to main
main:        default via <uplink gw> (networkd, metric by uplink priority)
```

- **IPv6:** the same rules and tables are installed with `ip -6` when an IPv6
  path exists (4.4).
- **Kill switch OFF, in `DIRECT` state:** rules 100 and 101 are swapped to
  `lookup main`.
- **WireGuard disabled:** there is no table 51820 and clients use `main`.
- **Routing is not the guarantee; the firewall is (4.4).** Both are applied,
  so a routing mistake can't leak.
- **Endpoint handling:**
  - `wg0` sets `fwmark 0x5182`, so encrypted packets always leave via the uplink.
  - The endpoint hostname is re-resolved by `trd` over the uplink resolver on
    every uplink change and on handshake failure, because the kernel doesn't
    re-resolve.
  - `PersistentKeepalive=25` is the default so the tunnel survives NAT and
    uplink roaming.
- **MTU/MSS:** `wg0` MTU defaults to **1380** (Q7; it tolerates PPPoE and
  tunneled hotel uplinks at about 3% cost), is configurable and honours `MTU=`
  from the `.conf`. SYN MSS is clamped to `rt mtu` on `wg0` and the uplinks.

### 4.4 Firewall (nftables, one atomic ruleset)
- **Rendering:** `trd` renders the whole ruleset from its state and applies it
  with a single `nft -f` transaction (flush and rebuild together). There are
  never partial rules.
- **Chain `input` (policy drop):**
  - Allowed: `lo`, established, DHCP/DNS/HTTP(S) on `ap0`/`ap0g`, ICMP.
  - Guests get DHCP/DNS only (no admin UI).
  - On uplinks: established flows and DHCP replies only.
  - Nothing is accepted from `wg0` except established.
  - **The admin UI is therefore unreachable from the uplink and from the tunnel.**
- **Chain `forward` (policy drop):**
  - `ap0` → `wg0`: accepted when WireGuard is enabled and the state allows it.
  - `ap0` → uplink: accepted **only** when WireGuard is disabled, or in the
    kill-switch-OFF `DIRECT` state, or in `PORTAL` (4.6) restricted to
    TCP 80/443 and to UDP/TCP 53 towards the uplink's DNS.
  - `ap0g` ↔ `ap0`: dropped.
  - Everything else: dropped.
  - IPv4 NAT masquerades on the egress interface.
- **DNS:**
  - Clients are handed `10.8.0.1`, and any UDP/TCP 53 to other addresses is
    redirected to it.
  - dnsmasq forwards to the tunnel DNS from the `.conf` `DNS=` line, or to a
    configured resolver reached via `wg0`. The rule for that resolver's /32
    points at `wg0`, so **if the tunnel is down, DNS queries die inside `wg0`
    and never reach the uplink**.
  - In `DIRECT` or no-VPN mode, dnsmasq is switched (SIGHUP with new config)
    to the uplink's DNS.
  - Clients' DoH/DoT just follows the forwarding policy, so it's tunnelled or
    dropped like any other traffic.
- **Device's own traffic:** a second resolver listens on 127.0.0.53 and uses
  the uplink's DNS. Only these flows leave via the uplink:
  - WireGuard UDP
  - NTP
  - HTTP time and portal probes
  - DNS for those
  - DHCP
  - the opt-in update check
- **IPv6 (clients get IPv6):**
  - **Hotspot side:**
    - On first boot each device generates a random ULA `/48` (RFC 4193) and
      puts `fdXX:XXXX:XXXX:1::/64` on `ap0` (`:8::/64` on `ap0g`).
    - dnsmasq sends router advertisements (SLAAC plus RDNSS, no DHCPv6),
      which Android also needs.
  - **Egress** uses NAT66 (masquerade), because hotels, eduroam and most
    WireGuard servers don't hand us a routable prefix to pass on:
    - via `wg0`, when the tunnel config has an IPv6 `Address` and `::/0` in
      `AllowedIPs`;
    - via the uplink, in `DIRECT`/no-VPN mode, when the uplink has global
      IPv6.
  - **No IPv6 path means no advertised route:** when neither path has IPv6,
    router advertisements go out with router lifetime 0. Clients keep
    link-local/ULA for local traffic but get no IPv6 default route, so there
    is no black-holing and they use IPv4.
  - **Same kill-switch policy:** one `inet`-family table covers IPv4 and IPv6,
    so the kill-switch and portal rules apply to both identically. With kill
    switch ON, `ip6` forwarding goes only to `wg0`.
  - **Known limitation:** clients whose only IPv6 address is a ULA prefer IPv4
    for dual-stack sites (RFC 6724 address selection). IPv6-only sites work.
    If your WireGuard server routes a real `/64` to the device, a later option
    can hand it out instead of NAT66.
  - **Endpoint:** the device may reach the WireGuard endpoint over IPv4 or IPv6
    on the uplink.
- **Boot ordering:**
  1. `trd-firewall-early.service` loads the **lockdown ruleset** (input:
     lo + established only; forward: drop; output: allow) and runs
     `Before=network-pre.target`.
  2. Then `sysctl net.ipv4.ip_forward=1`. Forwarding is shipped off in
     `sysctl.d` and turned on only after the lockdown ruleset is in place.
  3. hostapd, networkd and `trd` start only after that unit succeeds.

  If `trd` crashes, the last ruleset stays in force. It is fail-closed and
  never flushed on exit. systemd restarts `trd`.

### 4.5 Uplink / tunnel / kill-switch state machine

States, all owned by `trd`:

| State | Client forwarding |
|---|---|
| `LOCKDOWN` | None (boot, before the first render) |
| `NO_UPLINK` | None |
| `TIME_SYNC` | None when WireGuard is enabled. **Kill switch OFF skips this**: goes to `DIRECT` right away and returns to the tunnel once time is synced. |
| `TUNNEL_CONNECTING` | KS ON: none. KS OFF: `DIRECT` rules. |
| `TUNNEL_UP` | Only via `wg0` |
| `TUNNEL_DOWN` | KS ON: none. KS OFF: see `DIRECT`. |
| `DIRECT` | Via uplink (only with KS OFF, or with WireGuard disabled) |
| `PORTAL` | Via uplink, ports 80/443/53 only, time-limited (4.6) |

Transitions (the diagram is in `docs/architecture.md` once this doc is
approved):

```
LOCKDOWN → NO_UPLINK             (ruleset rendered)
NO_UPLINK → TIME_SYNC            (uplink has IPv4 + default route)
TIME_SYNC → TUNNEL_CONNECTING    (time trusted, or 60 s elapsed: try with the saved floor)
TUNNEL_CONNECTING → TUNNEL_UP    (healthy, see below)
TUNNEL_UP → TUNNEL_DOWN          (unhealthy, see below)
TUNNEL_DOWN → TUNNEL_UP          (healthy AND hold-down elapsed)
TUNNEL_DOWN → DIRECT             (KS OFF only; immediate)
DIRECT → TUNNEL_UP               (KS OFF: healthy for ≥30 s; flushes client conntrack)
any → NO_UPLINK                  (uplink lost; client conntrack flushed)
any(with uplink) → PORTAL        (user action in UI; only if a portal is detected)
PORTAL → TIME_SYNC               (probe returns 204, or timeout/cancel; policy restored first)
```

**What "down" means.** WireGuard has no connection state, so we define health
ourselves. Every 5 s we evaluate:
- `hs_age`: time since the peer's latest handshake, read over netlink. The
  protocol rekeys every 120 s and rejects sessions older than 180 s.
- `probe`: an ICMP echo to the tunnel **probe address**, sent with the
  tunnel table forced. It defaults to the first `DNS=` address in the `.conf`
  (usually the server's tunnel IP) and can be configured.

The tunnel is **healthy** when `hs_age ≤ 180 s` and the last 3 probes
succeeded. It is **unhealthy** after 3 consecutive probe failures (about 15 s),
or when `hs_age > 180 s` while traffic is being sent.

**Hysteresis:**
- `TUNNEL_DOWN → TUNNEL_UP` needs 3 successful probes in a row.
- For kill switch OFF, going back from `DIRECT` to the tunnel needs 30 s of
  continuous health. The hold-down doubles (up to 5 min) if more than 3 flaps
  happen within 10 minutes.
- With kill switch ON, hysteresis only affects what the UI shows. Traffic is
  always pinned to `wg0`, so safety doesn't depend on timing.

**No leaks during changes:**
- Every state change is one atomic `nft -f`, applied **before** routing rules
  change when tightening, and **after** they change when loosening.
- Client conntrack entries are flushed whenever the egress for clients changes,
  so NATed flows can't linger on the old path.
- Changing kill-switch mode, importing a new WireGuard config or changing the
  uplink passes through a `LOCKDOWN` render first.

### 4.6 Captive portals

**Detection** (device traffic, over the uplink): on every uplink change, and
every 10 min while the tunnel is unhealthy:
- HTTP GET `http://connectivitycheck.gstatic.com/generate_204` and
  `http://captive.apple.com/hotspot-detect.html`. The probe URLs are
  configurable, so users who don't want Google or Apple URLs can change them.
- The uplink's DHCP option 114 (RFC 8910) is read when present.

A redirect or unexpected body counts as a portal. The UI shows a banner with
the portal's URL.

**Approaches compared:**

| Approach | Works with | Client CNA appears? | Leakage | Cost | Verdict |
|---|---|---|---|---|---|
| **Temporary pass-through** | Any portal (JS, OAuth, payment, T&C). Portals that authenticate the device's MAC or IP work because clients are NATed behind it. | **Yes**: clients' own probes reach the real portal | Client HTTP/HTTPS/DNS go direct during the window (time-limited, user-initiated, ports 80/443/53 only) | Just firewall rules | **Recommended** |
| Proxy portal pages through our UI | Simple HTTP-only portals | No | None | Breaks on HTTPS, cookies scoped to the portal domain, JS redirects, OAuth | Rejected |
| Remote browser (headless Chromium on the Pi, streamed to the UI) | Nearly all | No | **None** (only the device talks to the portal) | +150 to 250 MB image, about 300 MB+ RAM, significant code | Possible later as a "zero-leak portal login" mode, not v1 |

**Portal mode flow (recommended design):**
1. A portal is detected and the tunnel can't come up. With kill switch ON,
   clients get no internet. The hotspot then shows a **"login required"
   notice**:
   - dnsmasq answers the OS probe hostnames (`captive.apple.com`,
     `connectivitycheck.gstatic.com`, etc.) with `10.8.0.1`.
   - `trweb` returns a small page: "The hotspot's uplink needs a login. Tap to
     start portal mode (up to 10 min)."
   - Clients also get DHCP option 114 pointing to the RFC 8908 API on `trweb`.

   iOS, macOS and Android should then show their "Log in to network" sheet
   **with our notice page** automatically (to verify in M5).
2. You tap **Start**, in that sheet or in the UI, and `trd` enters `PORTAL`:
   - The ruleset allows `ap0` → uplink for TCP 80/443 and DNS.
   - dnsmasq forwards to the uplink DNS and stops spoofing the probe names.
   - A countdown starts: 10 min by default, 30 min maximum, extendable.
   - The notice page then navigates to a plain-HTTP URL, which the upstream
     portal intercepts, so you log in inside the same sheet.
   - Devices that didn't show a sheet can open any `http://` URL, or use the
     portal link in our UI.
3. `trd` probes over the uplink every 5 s. When the probe returns 204, the
   portal is done: `trd` applies the normal ruleset **first** (atomic), flushes
   client conntrack, then moves to `TIME_SYNC` and the tunnel comes up.
   Timeout or cancel restores the normal ruleset the same way.
4. The notice page and the UI warn that during portal mode, web and DNS
   traffic from **all** hotspot devices bypasses the tunnel. Option Q5 limits
   it to the device that started it.

**Re-login:** portals that expire sessions are detected by the next failed
tunnel health check plus a portal probe, and the flow repeats.

### 4.7 Time
- The Pi 4 has no RTC; the Pi 5's RTC needs its battery.
- **Time floor** = max(image build time, `/data/clock` saved every 15 min and
  at shutdown). It is applied at boot before anything else, so certificate
  validity checks (EAP) and WireGuard handshake timestamps start from a sane
  time.
- **Sync:** `systemd-timesyncd` does NTP via the uplink. **If NTP is blocked**
  (no answer in 20 s), `trd` falls back to the HTTP `Date` header from three
  plain-HTTP hosts. It accepts a time only when at least 2 agree within 5 s
  and the time is at or after the floor.
- **Tunnel dependency:** the tunnel waits for synced time, with a 60 s cap,
  because WireGuard servers reject handshakes whose timestamp isn't newer than
  the last one they accepted.

### 4.8 Client behaviour
- **Private or rotating MAC addresses** only produce new leases; the pool
  covers about 230 addresses and leases last 1 h. Client counts come from
  associations.
- **Client-to-client traffic** (AirPlay, mDNS) works by default on `ap0`.
- **Guest mode (stretch):** a second BSS `ap0g` with its own SSID and password,
  `ap_isolate=1`, and firewall isolation from `ap0`. It shares the kill-switch
  policy. The MT7612U allows 2 interfaces, per its `iw list` combinations.
- **Metered toggle (stretch):**
  - **Android: verified.** dnsmasq sends `dhcp-option=43,"ANDROID_METERED"`,
    the string Android's own tethering DHCP server sends in option 43 when the
    network is metered (AOSP `NetworkStack` `DhcpPacket.java`). That Android
    clients act on it is to verify on your phone.
  - **Apple: I found no verified beacon-level or DHCP hint.** iOS turns on Low
    Data Mode automatically for Apple Personal Hotspot, but the mechanism
    isn't documented, and imitating Apple's vendor elements would be fragile
    and misrepresent the network. So Apple devices get only the one-time UI
    reminder to turn on Low Data Mode for this network.

## 5. First boot, admin access, factory reset

**First boot:**
1. The data partition is grown and initialised. The device generates its
   per-device ID, TLS key/cert, session secret and the random default hotspot
   PSK (shown after setup).
2. The **setup network** comes up as `travelrouter-setup-XXXX` (XXXX from the
   adapter MAC):
   - **Open by default**, with a captive-portal redirect to the setup page, so
     phones pop the setup screen automatically.
   - If you created `setup.txt` with `setup_password=…` on the FAT partition
     the card shows after flashing, the setup network uses WPA2/WPA3 with that
     password instead. This is recommended in crowded places.
3. **Claim-once:** the first client to set the admin password (at least 10
   characters, checked against a small built-in list of common passwords)
   claims the device. The setup network then shuts down and the configured
   hotspot starts.
4. **Setup window:** the setup network closes 30 min after boot if nobody has
   claimed the device, and reopens on the next power cycle. After claiming,
   the setup network never returns unless you factory reset.

**Admin UI:**
- Reachable only on `ap0` (see 4.4), at `http://10.8.0.1` and
  `http://travelrouter.lan`.
- **HTTP by default.** Settings → Security → HTTPS offers two choices:
  - **Generated certificate:** a per-device self-signed certificate for
    `travelrouter.lan` and `10.8.0.1`. The UI shows its SHA-256 fingerprint so
    you can check it in the browser warning, and a "regenerate" button.
  - **Upload:** a PEM certificate plus key, or a chain. It's checked for a key
    match, validity dates and a name matching `travelrouter.lan`/`10.8.0.1` or
    a custom hostname you enter.

  An optional "redirect HTTP to HTTPS" switch is available. No HSTS, so turning
  HTTPS off can never lock you out.
- Sessions: HttpOnly, SameSite=Strict cookies with CSRF tokens, 12 h idle
  timeout.
- Login back-off: exponential per client IP, with a global lockout of 5 min
  after 20 failures.

**Factory reset:**
1. In the UI (asks for the password again).
2. **Offline:** put the SD card in any computer, create an empty file named
   `factory-reset` on the visible partition, and boot. `trd` wipes `/data` and
   deletes the file.
3. **Optional hardware button:** short GPIO3 (pin 5) to GND (pin 6) for 10 s
   while running. A momentary button can be wired there.

The Pi 5's power button is left at its default behavior.

## 6. WireGuard
- **Import:** paste or upload a wg-quick `.conf`, or scan its QR code with the
  phone's camera using the file picker.
  - `[Interface]`: `PrivateKey`, `Address`, `DNS`, `MTU` are read.
  - `[Peer]`: `PublicKey`, `PresharedKey`, `Endpoint`, `AllowedIPs`,
    `PersistentKeepalive` are read.
  - **`PreUp/PostUp/PreDown/PostDown`, `Table` and `SaveConfig` are rejected**,
    and never executed.
  - `AllowedIPs` must contain `0.0.0.0/0`. Anything narrower is refused in v1
    and shown as an error.
- **Manual entry** also works, including generating a key pair on the device
  (the private key never leaves it; the public key is shown with a QR code for
  the server).
- `docs/wireguard-server.md` will cover setting up a compatible server:
  - a Debian server with `wg-quick`, forwarding and NAT
  - a DNS resolver on the tunnel IP
  - the peer section to add

  It does not cover a mobile VPN app.

## 7. Status and diagnostics
- **Uplink:** type, SSID/BSSID, band/channel, signal, security (AKM/PMF as
  negotiated), IPv4, gateway and DNS. The eduroam identity is shown masked.
- **Tunnel:** state-machine state and reason, last handshake age, rx/tx,
  endpoint, current kill-switch mode.
- **Clients:** associated stations with hostname, IP, signal and rate. MACs
  are partially masked in exports.
- **Portal:** detected URL and mode countdown.
- **Logs:** last 2,000 lines from journald (RAM only), redacted, with a filter.
- **Diagnostics export:** a `.tar.gz` of redacted configs, state-machine
  history, `ip addr/route/rule`, `nft list ruleset`, `iw` info and station
  dump, `wg show` without keys, versions and logs. Secrets and identities are
  replaced with `<redacted>`, and a test checks this (8.1).
- **"Scan eduroam security" button:** lists the AKM suites and PMF flags
  advertised by the uplink networks found in a scan. This answers the
  Rutgers WPA2-vs-WPA3 question in M3.

## 8. Testing strategy (hardware-independent)

### 8.1 Unit tests (Go, run everywhere)
- **Golden-file tests** for every generated config: hostapd, wpa_supplicant
  (every security type, including the Enterprise must-have-CA rule and the
  domain-match mapping), dnsmasq, nftables per state, WireGuard.
- **wg-quick parser:** table tests plus a Go fuzz target.
- **State machine:** driven by scripted events and a fake clock (flapping,
  hold-down doubling, portal timeout, `TIME_SYNC` fallback).
- **Redaction:** diagnostics export contains no secret from a fixture config
  (grep assertion).

### 8.2 Network-namespace integration (CI, stock `ubuntu-24.04` runners with sudo)

```
[client ns] --veth "ap0"-- [router ns: real nft ruleset, policy routing, dnsmasq, wg0, trd]
                                   --veth "eth0"-- [uplink ns: NAT GW, DNS, HTTP portal emulator, tcpdump]
                                                        -- [vpn ns: WireGuard peer, DNS, echo/HTTP]
```

`trd` runs with a `--sim` flag that replaces hostapd and wpa_supplicant with
veth links. Each test captures packets on the uplink veth and **asserts that
no client-sourced packet other than WireGuard UDP to the endpoint appears**
(the leak test):
- Boot: traffic generated before `trd` starts is dropped.
- KS ON, tunnel healthy: traffic goes via `wg0`; client DNS reaches the vpn ns
  only.
- KS ON, tunnel blocked at the uplink: client TCP, UDP, DNS and ICMP are all
  dropped; nothing leaks.
- KS OFF, tunnel blocked: fallback to direct after hysteresis; returns after
  30 s of health.
- Uplink change (swap uplink ns addressing): no leak during the swap;
  endpoint re-resolved.
- Mode switch ON↔OFF and config reimport during active traffic: no leak.
- Portal: emulator redirects until "login", pass-through allows only
  80/443/53, the ruleset is restored before the tunnel is up, and the timeout
  path works.
- IPv6: with kill switch ON, client IPv6 goes only via `wg0`. With no v6 path, RAs carry lifetime 0. In `DIRECT` mode, NAT66 goes via the uplink. A tunnel without IPv6 never lets client v6 reach the uplink.
- MSS: SYNs through `wg0` carry the clamped MSS.
- Time: NTP blocked plus HTTP Date fallback; the tunnel waits.

### 8.3 Wi-Fi simulation with `mac80211_hwsim`, where the runner kernel provides it
- hostapd AP ↔ wpa_supplicant STA for WPA2-PSK, WPA3-SAE and transition mode.
- hostapd acting as the EAP server (its internal EAP server supports TTLS/PAP,
  PEAP/MSCHAPv2 and TLS) with a throwaway test CA:
  - right CA plus right domain → connects
  - wrong CA → fails
  - wrong server name → fails
  - no CA → our generator refuses to render
- If `modprobe mac80211_hwsim` isn't available on the hosted runner, this job
  runs inside a small QEMU VM with a Debian kernel. It is never silently
  skipped: the job fails loudly if neither is possible.

### 8.4 On your hardware
Each milestone ends with exact commands and the output to paste back. For
example, M1 includes the 10-client gate: `iw dev ap0 station dump | grep -c
^Station` with every device associated, plus a USB power-meter reading at idle
and during `iperf3`.

## 9. Repository layout

```
cmd/trd/            privileged agent (state machine, renderers, drivers)
cmd/trweb/          web server + API
internal/config/    schema, validation, atomic store
internal/render/    hostapd / wpa_supplicant / dnsmasq / nft / wg generators (+ testdata golden files)
internal/fsm/       uplink/tunnel/portal state machine
internal/wgconf/    wg-quick parser
web/                static UI (HTML/CSS/ES modules)
image/              rpi-image-gen config, overlays, systemd units, sysctl, partition layout
test/netns/         namespace harness + leak tests
test/hwsim/         hwsim/EAP tests, test CA generator
docs/               architecture, eduroam (Rutgers example), wireguard-server, compatibility table, updating, redistribution
.github/workflows/  ci.yml, release.yml
LICENSE, LICENSES/THIRD_PARTY.md, CONTRIBUTING.md, SECURITY.md
```

## 10. License and redistribution
- **Our code:** **Apache-2.0** (decided). We copy no code from RaspAP
  (GPL-3.0), Travelmate or esp32_nat_router.
- **The image aggregates** Debian and Raspberry Pi packages under GPL-2.0,
  GPL-3.0, LGPL, MIT and others, plus binary firmware (Broadcom/Cypress Wi-Fi,
  Pi boot firmware, MediaTek MT7612U from linux-firmware). Redistributing it
  requires:
  1. Publishing corresponding source for GPL/LGPL packages. CI builds
     `sources-<ver>.tar` with `apt-get source` for the exact installed
     versions and attaches it to each release.
  2. Shipping every package's copyright file (kept under
     `/usr/share/doc/*/copyright` in the image) and listing firmware licenses
     in `LICENSES/THIRD_PARTY.md`. **In M1 I'll verify that each firmware
     license permits binary redistribution.**
  3. Not calling the image "Raspberry Pi OS" (trademark). It is "travelrouter,
     based on Debian and Raspberry Pi OS packages".
  4. GPLv3 installation information: users can always reflash the SD card and
     build their own image from the documented tooling.

## 11. Milestones (unchanged order, with what each proves)
1. **Image boots to setup network + UI.** CI image build, first boot, claim,
   hotspot. Gates: ≥10 clients on MT7612U; measured power.
2. **Uplink + NAT:** Ethernet, then Wi-Fi PSK and open networks; status page
   basics; 2.4 GHz coexistence measurement.
3. **WPA-Enterprise / eduroam:** TTLS/PAP, PEAP, TLS; CA and server name;
   security scan; Rutgers WPA2/WPA3 observation.
4. **WireGuard + kill switch:** full state machine, netns leak suite, time sync.
5. **Captive-portal mode:** notice page, pass-through, device CNA checks.
6. **Hardening + docs:** A/B updates, signing, factory reset paths,
   diagnostics, compatibility table, redistribution, README 15-minute path.
7. **Stretch:** guest SSID, metered toggle, zero-leak portal login (remote
   browser), routed IPv6 prefix instead of NAT66.

## 12. Requirements I'd change
1. **Battery:** the goal of 12 h on a common bank becomes **about 12 h on a
   20,000 mAh bank (Pi 4)**, as you chose. The README states this plainly.
2. **"No leaks during mode switches" gets one documented exception:**
   user-initiated portal mode, time-limited and restricted to 80/443/53. It
   can't be leak-free while still letting phones' own portal sheets appear.
   The remote-browser mode stays available later for zero-leak portal logins.
3. **Client IPv6 uses ULA plus NAT66** rather than a routed prefix, because
   travel uplinks don't delegate prefixes. Clients prefer IPv4 for dual-stack
   sites.
4. **The Apple beacon-level metered hint is dropped:** there is no verified
   mechanism. Apple devices get the reminder only.
5. **The hotspot defaults to WPA2/WPA3 transition mode with PMF optional**,
   plus a "WPA2 only" switch for old devices. No change in intent, just making
   it explicit.

## 13. Open questions (consolidated)

**Decided (revision 2):**
- Q1: Apache-2.0. The name is still open; `travelrouter` is the working name.
- Q2: private for now, moving to a public repo later (see 2.1).
- Q4: HTTP by default; HTTPS setting with a generated or uploaded certificate.
- Q6: IPv6 for clients (ULA plus NAT66, section 4.4).
- Q8: manual upload, plus an opt-in GitHub check.
- Q9: SSH off by default, with a setting to turn it on.
- Q10: `10.8.0.0/24`, with automatic fallback to `10.9.0.0/24` on collision.
- Q11: Pi 4B, Pi 5 and Zero 2 W all supported. Hotspot adapters: MT7612U
  (reference) and TP-Link AC1300 (RTL8812BU/AU, at risk until the M1 gate).

**Still open:**
- **Q1b. Project name.**
- **Q3. Setup network security.** Open with a 30-minute claim window
  (default), with `setup.txt` optional? Or `setup.txt` required?
- **Q5. Portal mode scope.** All hotspot devices, or only the device that
  started it?
- **Q7. WireGuard MTU.** 1380 or 1420?
- **Q10b.** Does your WireGuard server use `10.8.0.0/24` for tunnel
  addresses? If so, the hotspot will fall back to `10.9.0.0/24`. You could
  pick a different hotspot subnet instead.
- **Q12. Ethernet role.** Uplink only, a client port, or a setting to choose?
