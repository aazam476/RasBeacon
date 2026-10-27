# Design: open-source portable Wi-Fi travel router

- Status: **Implemented** (revision 4, the design the milestones followed).
  It is kept as the record of what was decided and why, and says what was
  built; [architecture.md](architecture.md) and [api.md](api.md) describe
  RasBeacon in more detail.
- Date: 2026-09-30
- Depends on: [ADR 0001](decisions/0001-platform.md), which is accepted
- Project name: **RasBeacon**. Daemons are `rbd` (privileged agent) and
  `rbweb` (web UI). The license is **Apache-2.0**.

Where this doc says "verified", the evidence is cited in ADR 0001 or here.
"To verify" marks items that can only be settled on your hardware or on
eduroam, and says which milestone checks them.

---

## 1. Hardware baseline

| Part | Role | Notes |
|---|---|---|
| Raspberry Pi 4B (2 GB or more) or Pi 5 | Board (reference) | arm64. Pi 5 has an RTC (needs its optional battery to keep time while off). |
| Raspberry Pi Zero 2 W | Board (**supported**, lower throughput) | 512 MB RAM. Onboard radio is 2.4 GHz only (about 33 Mbps measured), so the Wi-Fi uplink ceiling is about 30 Mbps through the tunnel. No Ethernet. The adapter plugs in through a micro-USB OTG adapter. Lowest power draw (ADR §5, config A). |
| Onboard CYW43455 (Pi 4/5) or BCM43436 (Zero 2 W), brcmfmac | **Wi-Fi uplink** | WPA2-Personal/Enterprise, open. WPA3 depends on firmware (M3 checks this). |
| Built-in Gigabit Ethernet (Pi 4/5), or a USB Ethernet adapter | **Role setting:** *Uplink* (default) or *Client port* (4.1) | |
| USB adapter with MT7612U (mt76x2u), e.g. Alfa AWUS036ACM | **Hotspot** (reference): 2.4 GHz default, 5 GHz optional | 128-entry station table. SAE and MFP supported. 2 AP interfaces (main + guest). |
| TP-Link "AC1300" USB adapters (Realtek, rtw88 driver) | **Hotspot** (supported, **at risk**) | TP-Link sells several AC1300 models with different chips: **Archer T3U, T3U Plus and T4U v3 use RTL8812BU**; **Archer T4U v1/v2 use RTL8812AU**. Both drivers are in the Raspberry Pi kernel (6.18, `CONFIG_RTW88_8822BU` and `CONFIG_RTW88_8812AU`), with a 32-entry station table and MFP (needed for WPA3). **Known risk:** users have reported the RTL8812BU dropping clients in AP mode ("failed to download beacon", "timed out to flush queue"). The ≥10-client M1 gate runs on this adapter too. Run `lsusb` to identify yours: `2357:012d`, `2357:0138` and `2357:0115` are RTL8812BU; `2357:0101`, `2357:0103` and `2357:010d` are RTL8812AU. If an RTL8812BU adapter fails the M1 gate, `rbd` first tries the reported mitigation (a longer beacon interval, plus a driver reload on beacon errors), and the fallback is an MT7612U adapter. |
| 5 V / 3 A USB-C supply, or a USB power bank | Power | Either works: RasBeacon has no battery and no battery-life target. Power banks that people test are listed in `docs/compatibility.md` (auto-off threshold, pass-through charging). |

Roles are assigned automatically:
- A mac80211 USB adapter that supports AP mode becomes `ap0`.
- The onboard radio becomes `wlan0`.
- Ethernet is `eth0`.


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
    `systemd-networkd`, `systemd-timesyncd`, `iw`, `wireless-regdb`
  - Our two binaries and static web assets

  **Removed:** NetworkManager, desktop/X, Bluetooth services, avahi, and
  cloud-init. **SSH** is installed but **off by default**, with a setting in
  the UI to turn it on (key-only, hotspot side only).
- **CI:** GitHub Actions.
  - `ci.yml` runs on every push and PR: Go vet and tests, golden-file tests,
    network-namespace integration tests, and hwsim tests (section 8).
  - `release.yml` runs on version tags and builds the image. It uses
    `ubuntu-24.04-arm` runners, which are free only for **public** repos. This
    In a private repository, `release.yml` can be started by hand, and it falls back to an x86
    runner with QEMU user emulation, which is slower. Nothing in the code or
    workflows hard-codes the repository name, so the move is a plain push.
- **Release artifacts:**
  - `rasbeacon-<board>-<ver>.img.xz`, one per board: flash with Raspberry
    Pi Imager ("Use custom") or balenaEtcher.
  - `rasbeacon-<board>-<ver>.rbupdate`: the signed update (Pi 4/5; the file
    carries its own Ed25519 signature, see section 9).
  - `SHA256SUMS`.
  - `rasbeacon-<board>-<ver>.packages.txt`: every package in the image with
    its source package and version, for the GPL/LGPL sources (section 10).

  Every feature is in the single image. There are no variants.

### 2.2 Partition layout (GPT, A/B "tryboot")

As built in milestone 1, this uses rpi-image-gen's `image-rota` layout
(see `image/README.md`), which implements the design below.

| Partition | FS | Size | Contents |
|---|---|---|---|
| `BOOTCONFIG` | FAT | 32 MB | A/B boot selection (`autoboot.txt`), plus the user-visible files `setup.txt` and `factory-reset` (section 5). Mounted at `/bootfs`. |
| `boot_a`, `boot_b` | FAT | 128 MB each | Firmware and kernel for each slot |
| `system_a`, `system_b` | erofs, **read-only** | 1536 MB each (768 MB before milestone 7) | The operating system for each slot |
| `persistent` | ext4 | 512 MB in the image; grows to fill the card on first boot | `/persistent`: RasBeacon settings in `/persistent/rasbeacon`, SSH host keys, per-slot `/var`, the swap file |

- `/var/log`, `/tmp` and `/run` are tmpfs, and journald is `Storage=volatile`.
  Nothing writes to flash except explicit config saves, the clock save,
  updates and the swap file (2 GB by default, adjustable from 0; with
  `vm.swappiness = 10` it is used only when memory is really short).
- **The `persistent` partition grows to fill the card** at boot
  (`rasbeacon-grow-data.service`: `sfdisk` moves the backup GPT to the end
  of the card and grows the last partition, then `resize2fs` grows the
  mounted file system). It does nothing once the partition fills the card.
- **Config writes are atomic:** write a temp file, `fsync`, `rename`, then
  `fsync` the directory. On abrupt power loss you get the previous or the new
  config, never a partial one.
- **Update flow, following the Pi firmware's documented A/B procedure:**
  1. Verify the bundle's Ed25519 signature and check that its version is not
     older than the running one (downgrades are refused; the way back is a
     flash) and that the running one is at least the update's
     `minFromVersion` (otherwise: back up, flash, restore).
  2. Write the inactive boot and root slots, verify their hashes, then run
     `reboot "0 tryboot"`.
  3. The new slot runs a health check a minute in: the web UI (`rbweb`) and
     the hotspot (`hostapd`) running, and `rbd` never restarted since boot
     (systemd's `NRestarts`). If it passes, `autoboot.txt` is swapped to
     commit the update. If it doesn't pass by 5 minutes after boot (counted
     from boot, so an `rbd` that keeps crashing still gets there), a normal
     reboot, which automatically returns to the old slot.
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
        │  HTTP(S) to 10.87.0.1 only
        ▼
rbweb   user "rbweb", only CAP_NET_BIND_SERVICE (ports 80/443), systemd-hardened
        (ProtectSystem=strict, NoNewPrivileges, AF_INET/AF_UNIX only)
        │  typed JSON-RPC over /run/rbd/api.sock (0660 root:rbweb)
        ▼
rbd     root, systemd-hardened; owns /persistent/rasbeacon; renders configs to /run/rbd;
        drives hostapd / wpa_supplicant (control sockets), nft, wg (netlink),
        networkd, dnsmasq
```

- **`rbweb`** serves the static UI and a REST/JSON API. It never reads
  secrets: secret fields are write-only and the API only reports
  "set / not set". It handles:
  - Sessions, login rate limiting and CSRF protection. Every request that
    checks the admin password (login, a new password, factory reset) goes
    through one gate: one check at a time, counted by the rate limit.
  - Answering only under RasBeacon's own names (its address,
    `rasbeacon.lan`, the owner's name for it), so a web page can't reach
    the API through DNS rebinding.
  - Admin password hashing (argon2id from `golang.org/x/crypto`; the only
    other third-party Go dependency is `rsc.io/qr`, for the Wi-Fi QR codes).
- **`rbd`** changes state under a single lock (one change at a time) and is the **only** writer of network state.
  It owns:
  - Validating and rendering configs.
  - The kill-switch/uplink/portal state machine (section 4.5).
  - Health probes, time sync and updates.
- **Language:** Go, stdlib-first. Each daemon is one static binary, uses about
  10 to 20 MB of RAM, cross-compiles trivially, and Go is familiar to
  contributors.
- **Frontend:** React and TypeScript, built with Vite (npm is used only to
  build it; the device runs no Node.js). Mobile-first, one screen per task,
  works offline, and all assets are served from the device.

Secrets:
- Stored in `/persistent/rasbeacon/secrets.json`, `root:root 0600` (directory 0700).
- Rendered into `/run/rbd/*.conf` at 0600.
- Never logged:
  - `wpa_supplicant` and `hostapd` run without `-d`.
  - `rbd` never logs passwords or keys, and the diagnostics report replaces
    every secret it knows with `<redacted>` (and hides half of each MAC
    address) before it is downloaded.
  - The eduroam identity is shown masked (`u***@example.com`) in logs and exports.

Release images contain no keys, passwords or per-device material. Everything
per-device is generated on first boot.

## 4. Network design

### 4.1 Interfaces and addressing

| Interface | Managed by | Address |
|---|---|---|
| `br-lan` (client side) = `ap0` + `eth0` when Ethernet is set to *Client port* | `rbd` (bridge), hostapd (`bridge=br-lan`) | `10.87.0.1/24`, plus an IPv6 ULA `/64` (4.4). If it collides with the uplink subnet **or the WireGuard tunnel's addresses**, `rbd` falls back to `10.88.0.1/24` (then `172.31.87.1/24`, `192.168.87.1/24`), restarts the hotspot so devices reconnect and get new addresses, and shows why in the UI. It doesn't move back while running. |
| `guest0` (guest network) in `br-guest` | hostapd second BSS (`bridge=br-guest`) | `10.87.1.1/24` (the /24 after the hotspot's); no IPv6 |
| `wlan0` (Wi-Fi uplink) | wpa_supplicant (driven by `rbd`) + networkd DHCPv4/RA | From uplink DHCP |
| `eth0` | *Uplink:* networkd DHCP client. *Client port:* bridged into `br-lan` | From uplink DHCP / none |
| `wg0` | `rbd` via netlink | From the `.conf` `Address` |

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
- **Ethernet role setting** (Settings → Ethernet): *Uplink* (default) or
  *Client port*. The setting is hidden when there is no Ethernet interface,
  as on a Zero 2 W without a USB Ethernet adapter.
  - *Client port* bridges `eth0` into `br-lan`. A wired laptop gets the same
    subnet, DHCP, kill switch and portal policy as Wi-Fi clients, and can
    reach phones for AirPlay and mDNS.
  - **Safety check:** before bridging, and on every link-up, `rbd` sends a
    DHCPDISCOVER on `eth0`. If a foreign DHCP server answers (you plugged a
    client port into a hotel wall jack), `eth0` stays out of the bridge. The
    UI then offers "This port looks like an uplink. Switch to Uplink?", so we
    never hand out addresses onto someone else's network.
- **DHCP (dnsmasq on `br-lan`, and `br-guest` for guests):**
  - Leases last 1 h and the pool is `.20` to `.250`. Leases are keyed on client
    identifier or MAC, so rotating private MAC addresses just get new leases.
  - The client count comes from **currently associated stations**
    (`hostapd` STA list), never from lease history.

### 4.2 Uplink management
- **Priority:** Ethernet with a link, then saved Wi-Fi networks in your order,
  scanning for any that are in range.
  - *As built in M2:* all saved networks go into `wpa_supplicant.conf` with
    descending `priority`, and wpa_supplicant itself picks the most preferred
    one in range and roams. This is simpler than `rbd` driving one network at
    a time and uses wpa_supplicant's tested selection logic. Ethernet wins by
    route metric (100 versus 600). If hardware testing shows wpa_supplicant
    sticking to a network with DHCP but no connectivity, M5's connectivity
    probes will add the "switch after 20 s" rule.
- **Enterprise uplinks:** `eap=TTLS` + `phase2="auth=PAP"`, `PEAP` +
  `MSCHAPV2`, or `TLS` with a client cert/key. Fields:
  - `identity` and `anonymous_identity`
  - `ca_cert`: the uploaded CA (PEM/DER; multiple CAs concatenated); the
    system's CA list when only a server name is given; or, with neither,
    `probe://` at the first connection and then
    `hash://server/sha256/<the server certificate's SHA-256>` ("trust on
    first connect", `internal/agent/servercert.go`)
  - `domain_suffix_match` (from the "server name" field;
    `*.radius.example.com` maps to `domain_suffix_match=radius.example.com`)
  - `ieee80211w=1` (PMF optional). `key_mgmt=WPA-EAP WPA-EAP-SHA256` covers
    WPA2- and WPA3-Enterprise.

  **The login server is always checked.** The CA certificate and server
  name are optional (under Advanced); without them the first connection's
  certificate is trusted, before any password is sent, and only that one
  afterwards. A "Don't check the login server" override exists only under
  Advanced and shows a persistent warning banner. The default is never "no
  validation".
- **No eduroam preset:** RasBeacon is a general travel router, so eduroam
  is added like any WPA-Enterprise network (from the scan, or Add manually),
  with TTLS/PAP as the default login method. `docs/wpa-enterprise.md`
  explains it, with eduroam as one example; nothing about it is built into
  the code.
- **Regulatory:** one country setting is applied to cfg80211 (`iw reg set`),
  hostapd and wpa_supplicant.

### 4.3 Routing
Tables and rules, with WireGuard enabled:

```
ip rule  90: fwmark 0x5182 lookup main          # WireGuard's own UDP goes via uplink
ip rule 100: iif br-lan lookup 51820            # all client traffic → tunnel table
ip rule 101: iif br-guest lookup 51820
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
  - The endpoint hostname is re-resolved by `rbd` over the uplink resolver on
    every uplink change and on handshake failure, because the kernel doesn't
    re-resolve.
  - `PersistentKeepalive=25` is the default so the tunnel survives NAT and
    uplink roaming.
- **As built in M4:**
  - Rule 90 and the fwmark are not needed: only traffic *arriving on
    br-lan* is sent to table 51820, and WireGuard's own UDP packets are
    created by the device itself, so they always use `main`.
  - In `DIRECT`, rule 100 is deleted rather than swapped (same effect).
  - The tunnel doesn't wait for the clock: it simply doesn't handshake
    until the clock is right, which the state machine already treats as
    "not working". The time floor and the HTTPS `Date` fallback (4.7) keep
    the clock right.
  - The health check is the handshake age (at most 180 s, with keepalive
    25 s); probes through the tunnel are left for later if hardware
    testing shows a need.
- **MTU/MSS:** `wg0` MTU defaults to **1380** (decided; it tolerates PPPoE and
  tunneled hotel uplinks at about 3% cost), is configurable and honours `MTU=`
  from the `.conf`. SYN MSS is clamped to `rt mtu` on `wg0` and the uplinks.

### 4.4 Firewall (nftables, one atomic ruleset)
- **Rendering:** `rbd` renders the whole ruleset from its state and applies it
  with a single `nft -f` transaction (flush and rebuild together). There are
  never partial rules.
- **Chain `input` (policy drop):**
  - Allowed: `lo`, established, DHCP/DNS/HTTP(S) on `br-lan`, DHCP/DNS on `br-guest`, ICMP.
  - Guests get DHCP/DNS only (no admin UI).
  - On uplinks: established flows and DHCP replies only.
  - Nothing is accepted from `wg0` except established.
  - **The admin UI is therefore unreachable from the uplink and from the tunnel.**
- **Chain `forward` (policy drop):**
  - `br-lan` → `wg0`: accepted when WireGuard is enabled and the state allows it.
  - `br-lan` → uplink: accepted **only** when WireGuard is disabled, or in the
    kill-switch-OFF `DIRECT` state, or in `PORTAL` (4.6) restricted to
    TCP 80/443 and to UDP/TCP 53 towards the uplink's DNS.
  - `br-guest` ↔ `br-lan`: dropped.
  - Everything else: dropped.
  - IPv4 NAT masquerades on the egress interface.
- **DNS:**
  - Clients are handed `10.87.0.1`, and any UDP/TCP 53 to other addresses is
    redirected to it.
  - dnsmasq forwards to the tunnel DNS from the `.conf` `DNS=` line, or to a
    configured resolver reached via `wg0`. The rule for that resolver's /32
    points at `wg0`, so **if the tunnel is down, DNS queries die inside `wg0`
    and never reach the uplink**.
  - In `DIRECT` or no-VPN mode, dnsmasq is switched (SIGHUP with new config)
    to the uplink's DNS, or to the owner's DNS servers (Internet → DNS
    servers). Those can be DNS-over-HTTPS servers: dnsmasq asks them through
    `rbdoh` on 127.0.0.1:5053, which bootstraps their host names with the
    owner's plain servers, then the uplink's DNS, and keeps the last known
    addresses as a fallback. A login page in the way switches back to the
    uplink's DNS (portals need it).
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
      puts `fdXX:XXXX:XXXX:1::/64` on `br-lan`. The guest network has no
      IPv6.
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
  1. `rasbeacon-firewall-early.service` loads the **lockdown ruleset** (input:
     lo + established only; forward: drop; output: allow) and runs
     `Before=network-pre.target`.
  2. Then `sysctl net.ipv4.ip_forward=1`. Forwarding is shipped off in
     `sysctl.d` and turned on only after the lockdown ruleset is in place.
  3. hostapd, networkd and `rbd` start only after that unit succeeds.

  If `rbd` crashes, the last ruleset stays in force. It is fail-closed and
  never flushed on exit. systemd restarts `rbd`.

### 4.5 Uplink / tunnel / kill-switch state machine

`internal/tunnel` decides, every few seconds, where devices' traffic goes
while the VPN is on. It has no side effects: rbd feeds it what it saw and
applies the answer (firewall, routing, DNS).

| State | Devices' traffic |
|---|---|
| `Off` | VPN off: directly through the uplink (with the kill switch on, nowhere; see section 6) |
| `Connecting` | VPN on, no working tunnel yet: into the tunnel, where it is dropped until it works |
| `Up` | Through the tunnel |
| `Down` | The tunnel worked, then stopped: into the tunnel, dropped |
| `Direct` | Kill switch off only, after the tunnel was broken for 30 s: directly through the uplink, until the tunnel works again for a while |

Transitions:

```
Off → Connecting                 (VPN turned on)
Connecting, Down → Up            (healthy)
Connecting, Down → Direct        (kill switch off, broken for 30 s)
Up → Down                        (unhealthy)
Direct → Up                      (healthy for the return delay: 30 s, doubling
                                  up to 5 min while the tunnel keeps breaking)
any → Off                        (VPN turned off)
```

Login mode for a hotel's page (4.6) sets the tunnel aside for new web and
DNS connections on its own, without changing the state.

**What "healthy" means.** WireGuard has no connection state, so health is
the age of the peer's latest handshake: the protocol renews its session
every 2 minutes while traffic (keepalives included) flows, so a handshake
older than 180 s means the server stopped answering.

**Hysteresis:**
- For kill switch OFF, going back from `Direct` to the tunnel needs the
  return delay of continuous health. The delay doubles (up to 5 min) each
  time the tunnel breaks again soon after, and resets after 10 minutes of
  working.
- With kill switch ON, timing only affects what the UI shows. Traffic is
  always pinned to `wg0`, so safety doesn't depend on timing.

**No leaks during changes:**
- Every change is one atomic `nft -f`, applied **before** routing rules
  change when tightening, and **after** they change when loosening.
- When the path for devices' traffic changes, connections on the old path
  stop at once: the firewall only accepts established connections on the
  *current* path, and a connection that left through the tunnel is
  labeled and may never leave anywhere else (tested in
  `test/netns/portal_test.go` and `routed_ipv6_test.go`; for NATed IPv4
  the kernel also ends a connection whose way out changed, but the routed
  IPv6 network isn't NATed). Nothing has to be flushed, so there is no
  moment where an old connection slips through.

### 4.6 Captive portals

**Detection** (device traffic, over the uplink): rbd checks on every uplink
change and every 10 minutes, every minute while the VPN is on but its
tunnel down, and every 5 s in login mode (`internal/agent/portal.go`):
- HTTP GET `http://connectivitycheck.gstatic.com/generate_204` and
  `http://captive.apple.com/hotspot-detect.html`. The two addresses are
  fixed, the same checks phones make.

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
     `connectivitycheck.gstatic.com`, etc.) with `10.87.0.1`.
   - `rbweb` returns a small page: "The hotspot's uplink needs a login. Tap to
     start portal mode (up to 10 min)."
   - Clients also get DHCP option 114 pointing to the RFC 8908 API on `rbweb`.

   iOS, macOS and Android should then show their "Log in to network" sheet
   **with our notice page** automatically (to verify in M5).
2. You tap **Start**, in that sheet or in the UI, and `rbd` enters `PORTAL`:
   - The ruleset allows `br-lan` → uplink for TCP 80/443 and DNS.
   - dnsmasq forwards to the uplink DNS and stops spoofing the probe names.
   - A countdown starts: 10 min by default, 30 min maximum, extendable.
   - The notice page then navigates to a plain-HTTP URL, which the upstream
     portal intercepts, so you log in inside the same sheet.
   - Devices that didn't show a sheet can open any `http://` URL, or use the
     portal link in our UI.
3. `rbd` probes over the uplink every 5 s. When the probe returns 204, the
   portal is done: `rbd` applies the normal ruleset **first** (atomic), and
   the tunnel comes up. Connections opened during login mode stop at once,
   because the firewall only accepts established connections on the current
   path. Timeout or cancel restores the normal ruleset the same way.
4. The notice page and the UI warn that during portal mode, **all** client
   devices may start web and DNS connections outside the tunnel (decided:
   all devices, so the login sheet can appear on whichever one is handy).
   Connections they opened through the tunnel before stay in it, and stop.

**Why the hole is needed, and why it isn't the same as turning the kill switch
off:**
- Until the portal login is done, the hotel network blocks everything except
  its login page. That includes WireGuard's UDP, so the tunnel can't come up.
- With the kill switch ON, clients may only use the tunnel, so no client can
  reach the login page, and the router can't get logged in from your phone.
- Something has to let a client reach the portal. There are three options:

| Option | What leaves outside the tunnel | Ends |
|---|---|---|
| Turn the kill switch OFF by hand | **Everything**, from all devices, on all ports | Only when you remember to turn it back on |
| **Portal mode (chosen)** | New web and DNS connections only (TCP 80/443, DNS) | Automatically, as soon as the login works or after 10 min |
| Zero-leak remote browser (stretch) | Nothing from clients: the router itself loads the portal and shows it to you inside our UI | When you finish |

  Portal mode is a narrower, self-closing version of turning the kill switch
  off. **The kill-switch setting itself stays ON the whole time.** If you'd
  rather never open a hole, the remote-browser option can later become the
  default, at the cost of RAM, image size, and no automatic login sheet on the
  phone.

Login mode lasts 10 minutes, extendable to 30, and ends by itself on a
passing check. It can be started by any device on the hotspot, from the
notice, without the admin password.

**Re-login:** portals that expire sessions are detected by the next failed
tunnel health check plus a portal probe, and the flow repeats.

### 4.7 Time
- The Pi 4 has no RTC; the Pi 5's RTC needs its battery.
- **Time floor** = max(image build time, `/persistent/rasbeacon/clock` saved every 15 min and
  at shutdown). It is applied at boot before anything else, so certificate
  validity checks (EAP) and WireGuard handshake timestamps start from a sane
  time.
- **Sync:** `systemd-timesyncd` does NTP via the uplink. **If NTP is blocked**
  (not synchronized 30 s after the uplink connects), `rbd` falls back to the
  `Date` header of three HTTPS servers (Google, Apple, Cloudflare), at most
  once an hour and never while a login page is in the way. Their
  certificates are checked as of the day each became valid, since the clock
  that would check the dates is the one being set; plain HTTP would let a
  network with a login page answer with any time. It accepts a time only
  when at least 2 agree within 5 s and the time is at or after the floor.
- **Tunnel dependency:** WireGuard servers reject handshakes whose
  timestamp isn't newer than the last one they accepted, so the clock must
  never go back: the time floor applied at boot sees to that, and the tunnel
  starts without waiting for NTP.

### 4.8 Client behaviour
- **Private or rotating MAC addresses** only produce new leases; the pool
  covers about 230 addresses and leases last 1 h. Client counts come from
  associations.
- **Client-to-client traffic** (AirPlay, mDNS) works by default on `br-lan`, between Wi-Fi and wired clients alike.
- **Guest network:** a second BSS `guest0` (in `br-guest`) with its own SSID and password,
  `ap_isolate=1`, and firewall isolation from `br-lan`. It shares the kill-switch
  policy. The MT7612U allows 2 interfaces, per its `iw list` combinations.
- **Metered toggle (stretch):**
  - **Android: verified.** dnsmasq sends `dhcp-option=43,"ANDROID_METERED"`,
    the string Android's own tethering DHCP server sends in option 43 when the
    network is metered (AOSP `NetworkStack` `DhcpPacket.java`). That Android
    clients act on it is to verify on your phone.
  - **Apple: there is no verified beacon-level or DHCP hint.** iOS turns on Low
    Data Mode automatically for Apple Personal Hotspot, but the mechanism
    isn't documented, and imitating Apple's vendor elements would be fragile
    and misrepresent the network. So Apple devices get only the one-time UI
    reminder to turn on Low Data Mode for this network.

## 5. First boot, admin access, factory reset

**First boot:**
1. The data partition is grown and initialised. The device generates its
   per-device ID, TLS key/cert, session secret and the random default hotspot
   PSK (shown after setup).
2. The **setup network** comes up as `RasBeacon-Setup-XXXX` (XXXX from the
   adapter MAC):
   - **Open by default**, with a captive-portal redirect to the setup page, so
     phones pop the setup screen automatically.
   - **`setup.txt` (optional; checked on every boot until the device is
     claimed):**
     - After flashing, the card shows one partition as a normal drive. If a
       file named `setup.txt` there contains `setup_password=<8 to 63
       characters>`, the setup network uses WPA2/WPA3 with that password
       instead of being open. This is recommended in crowded places.
     - If the file exists but can't be used (wrong format, or a password of
       the wrong length), the device **does not fall back to an open
       network**. The setup network stays off, the activity LED blinks an
       error pattern (3 short, 1 long), and the README troubleshooting section
       explains how to fix the file.
     - The UI shows which mode the setup network used.
     - After the device is claimed, `rbd` overwrites and deletes `setup.txt`,
       so the password doesn't sit in plain text on the card.
3. **Claim-once:** the first client to set the admin password (at least 10
   characters, checked against a small built-in list of common passwords)
   claims the device. The setup network then shuts down and the configured
   hotspot starts.
4. **Setup window:** the setup network closes 30 min after boot if nobody has
   claimed the device, and reopens on the next power cycle. After claiming,
   the setup network never returns unless you factory reset.

**Admin UI:**
- Reachable only from the client side, `br-lan` (see 4.4), at `http://10.87.0.1` and
  `http://rasbeacon.lan`.
- **HTTP by default.** Settings → Security → HTTPS offers two choices:
  - **Generated certificate:** a per-device self-signed certificate for
    `rasbeacon.lan` and `10.87.0.1`. The UI shows its SHA-256 fingerprint so
    you can check it in the browser warning, and a "regenerate" button.
  - **Upload:** a PEM certificate plus key, or a chain. It's checked for a key
    match, validity dates and a name matching `rasbeacon.lan`/`10.87.0.1` or
    a custom hostname you enter.

  With HTTPS on, plain HTTP redirects to HTTPS, and logins over plain HTTP
  are refused. No HSTS, but a browser that can't open the HTTPS page (one
  that refuses the certificate) can't log in to turn HTTPS off either:
  then the `factory-reset` file is the way back. So generated certificates
  are valid for 820 days (Apple's devices refuse more than 825) and renewed
  automatically 30 days before they run out.
- Sessions: HttpOnly, SameSite=Strict cookies with CSRF tokens, 12 h idle
  timeout.
- Login back-off: exponential per client IP, with a global lockout of 5 min
  after 20 failures.

**Factory reset:**
1. In the UI (asks for the password again).
2. **Offline:** put the SD card in any computer, create an empty file named
   `factory-reset` on the visible partition, and boot. `rbd` wipes `/persistent/rasbeacon` and
   deletes the file.
3. **Optional hardware button:** short GPIO 17 (pin 11) to GND (pin 9) for
   10 s while running. A momentary button can be wired there.

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
  return-delay doubling, falling back to `Direct`, turning off).
- **Redaction:** diagnostics export contains no secret from a fixture config
  (grep assertion).

### 8.2 Network-namespace integration (CI, stock `ubuntu-24.04` runners with sudo)

```
[client ns] --veth "ap0"-- [router ns: real nft ruleset, policy routing, dnsmasq, wg0, rbd]
                                   --veth "eth0"-- [uplink ns: NAT GW, DNS, HTTP portal emulator, tcpdump]
                                                        -- [vpn ns: WireGuard peer, DNS, echo/HTTP]
```

`rbd` runs with a `--sim` flag that replaces hostapd and wpa_supplicant with
veth links. Each test captures packets on the uplink veth and **asserts that
no client-sourced packet other than WireGuard UDP to the endpoint appears**
(the leak test):
- Boot: traffic generated before `rbd` starts is dropped.
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
cmd/rbd/            privileged agent (state machine, renderers, drivers)
cmd/rbweb/          web server + API
internal/config/    schema, validation, atomic store
internal/render/    hostapd / wpa_supplicant / dnsmasq / nft / wg generators (+ testdata golden files)
internal/fsm/       uplink/tunnel/portal state machine
internal/wgconf/    wg-quick parser
web/                static UI (HTML/CSS/ES modules)
image/              rpi-image-gen config, overlays, systemd units, sysctl, partition layout
test/netns/         namespace harness + leak tests
test/hwsim/         hwsim/EAP tests, test CA generator
docs/               architecture, wpa-enterprise, wireguard-server, compatibility table, updating, redistribution
.github/workflows/  ci.yml, release.yml
LICENSE, LICENSES/THIRD_PARTY.md, CONTRIBUTING.md, SECURITY.md
```

**Commit messages** start with a type prefix and a colon, with no scope in
parentheses (`docs: …`, not `docs(readme): …`), as `CONTRIBUTING.md` says:

| Prefix | Use for |
|---|---|
| `feat:` | New feature |
| `fix:` | Bug fix |
| `chore:` | Tooling, config, dependencies |
| `refactor:` | Code change that isn't a fix or feature |
| `docs:` | Documentation only |
| `style:` | Formatting, no logic change |
| `test:` | Adding or fixing tests |

## 9a. Code readability (top priority)

**The audience is a junior engineer or a 2nd–3rd year computer science
student.** When readability conflicts with cleverness or brevity, readability
wins.

- **Every file starts with a header comment**: what the file is for, where it
  fits in the system, and which files it works with.
- **Every function, type and constant has a comment.** It says what the thing
  does and why it exists; the "why" matters most. Non-obvious networking
  terms (NAT, conntrack, WireGuard handshake, PMF) link to `docs/glossary.md`.
- **Plain code over clever code:**
  - descriptive names (no one-letter names outside short loops)
  - small functions and early returns
  - no reflection or code generation
  - no deep inheritance or abstraction layers
  - Go: no goroutine tricks; `rbd` changes state under a single lock so state changes
    happen in one place, in order
- **Configs are template files, not string-building code.** The nftables
  ruleset, hostapd, wpa_supplicant and dnsmasq configs live as `.tmpl` files
  that read top to bottom, like the real config, with a comment on every rule
  or block explaining it.
- **Tests double as documentation.** Each kill-switch state has a golden file
  showing the exact firewall rules it produces, and test names describe
  behavior (e.g. `TestKillSwitchOn_TunnelDown_DropsClientDNS`).
- **Error messages say what went wrong and what to try next.**
- **Formatters and linters in CI:** `gofmt`, `go vet` and `staticcheck` for Go;
  the matching formatter and linter for the web UI.
- **Docs for newcomers:** `docs/architecture.md` (how the pieces fit, with
  diagrams), `docs/glossary.md`, and `CONTRIBUTING.md` (how to build, run the
  simulated network and add a test).

## 9b. Languages and web UI handoff

**Languages (decided):**
- Go for `rbd`, `rbweb` and the simulated-network tests.
- React + TypeScript + Vite for the web UI. It runs in the browser; the Pi only
  serves the built files, which are embedded in `rbweb`, so there's no Node on
  the device.
- nftables rules, hostapd/wpa_supplicant/dnsmasq configs as commented `.tmpl`
  files.
- Bash only inside image-build hooks and CI steps.

**The web UI was first built bare, then redesigned (M8).**
- The first UI (M1 to M7) was bare: semantic HTML, one small stylesheet,
  every state handled, logic in `use<Screen>.ts` hooks and markup in
  `<Screen>View.tsx` files.
- It was then redesigned after Apple's Settings app (React
  Router, Tailwind CSS; see `web/README.md`), adding features from its
  mockups. Its seam is one interface, `useDevice()`
  (`web/src/data/device.ts`): screens only read its state and call its
  actions. The demo provider (`web/src/data/demo/`) runs it on sample data;
  the real provider (`web/src/data/real/`) implements every action against
  the API, reusing the first UI's logic (`logic.ts`, unit-tested).
- **Integrating a restyled version:** keep `device.ts` and the wording the
  end-to-end test uses (`docs/ui-contract.md`); check that nothing outside
  `data/` talks to the API and that no new runtime packages came in; run
  the build, lint, format check, unit tests and the end-to-end test.
- **Ready for the v2 mobile app:**
  - The API is versioned under `/api/v1` and documented in `docs/api.md`.
  - The API accepts a token login alongside the browser's cookie sessions.
  - All rules live in `rbd`/`rbweb`, so the web UI and a future app behave
    identically.

## 10. License and redistribution
- **Our code:** **Apache-2.0** (decided). We copy no code from RaspAP
  (GPL-3.0), Travelmate or esp32_nat_router.
- **The image aggregates** Debian and Raspberry Pi packages under GPL-2.0,
  GPL-3.0, LGPL, MIT and others, plus binary firmware (Broadcom/Cypress Wi-Fi,
  Pi boot firmware, MediaTek MT7612U from linux-firmware). Redistributing it
  requires:
  1. Providing the corresponding source for GPL/LGPL packages. Each release
     lists every package's source package and exact version
     (`.packages.txt`); those sources stay available in the Debian and
     Raspberry Pi archives (snapshot.debian.org keeps every version), and a
     written offer covers anyone who can't get them there
     ([source-code.md](source-code.md)).
  2. Shipping every package's copyright file (kept under
     `/usr/share/doc/*/copyright` in the image) and listing firmware licenses
     in `LICENSES/THIRD_PARTY.md`. Each firmware license is checked
     for binary redistribution.
  3. Not calling the image "Raspberry Pi OS" (trademark). It is "RasBeacon,
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
   browser), routed IPv6 prefix instead of NAT66. Built; see 11b.
8. **The redesigned web UI** and the features from its
   mockups. Built; see 11c.

## 11a. As built in M6

- **Updates:** rpi-image-gen's `<image>.update.tar.zst` (boot and system as
  Android sparse images) is wrapped in a `.rbupdate` tar with a manifest
  and an Ed25519 signature over the manifest (which holds the bundle's
  SHA-256). rbd verifies the signature and board first, streams the bundle
  through `zstd` into `/dev/disk/by-slot/other/*`, verifies the SHA-256,
  then `systemctl reboot "0 tryboot"`. On a tryboot start, rbd confirms
  after 60 s of normal running, once `systemctl is-active rbweb.service
  rasbeacon-hostapd.service` succeeds (looking again every 15 s) and
  systemd's `NRestarts` for `rbd.service` is 0, by writing
  `rpi-slot-tryboot`'s output to `autoboot.txt`, or forces a plain reboot
  (back to the old slot) 5 minutes after boot (`/proc/uptime`, so a
  crashing rbd doesn't start the wait again at every restart). Keys: `cmd/rbsign`, the `RASBEACON_UPDATE_SIGNING_KEY`
  secret, and `/usr/lib/rasbeacon/update-keys/*.pub` in the image.
- **HTTPS:** as specified (generated or uploaded certificate). Setup, the
  portal notice and RFC 8908 stay on HTTP; logins over HTTP are refused
  while HTTPS is on.
- **Reset button:** GPIO 17 via the `gpio-key` overlay (KEY_RESTART), 10 s
  hold.
- **Not built:** API tokens for a future app (sessions are enough for the
  web UI), and the zero-leak remote-browser portal login.
- **Source code:** each release lists its packages with source versions;
  see `docs/source-code.md`.

## 11b. As built in M7

- **Guest network:** a second BSS (`guest0`) in the same hostapd, on its
  own bridge `br-guest` with the /24 after the hotspot's (10.87.1.0/24;
  `ChooseLANPrefix` checks both, so they move together). It needs a radio
  that can run two APs, read from `iw phy` "valid interface combinations"
  (`radio.MaxAPInterfaces`); its BSSID is the radio's MAC with the
  locally administered bit set. Guests follow the owner's devices' path
  exactly (routing rule 101 `iif br-guest`, the `lans` set in the
  firewall), so the kill switch covers them. Isolation: input from
  `br-guest` only for DHCP and DNS (everything else `reject`, so phones
  don't wait), no forwarding between `br-lan` and `br-guest`, and
  `ap_isolate=1`. IPv4 only. Same security mode as the hotspot; its own
  password (must differ). Tested in `test/netns/guest_test.go` and on
  simulated radios in `test/hwsim/guest_test.go`.
- **Metered:** `dhcp-option=43,ANDROID_METERED`, which Android reads.
  No hint exists for Apple or Windows devices, so the UI tells the owner
  what to set there. Changing only this setting restarts dnsmasq, not
  hostapd.
- **Routed IPv6:** only through the VPN. When the WireGuard server routes a
  public /64 to RasBeacon (`VPNConfig.RoutedIPv6Prefix`), rbd adds `::1`
  of it to `br-lan` while the VPN is in use; dnsmasq's
  `constructor:br-lan` announces it with the ULA. A rule in
  `postrouting_nat` exempts it from NAT on `wg0` only, so in direct mode
  or login mode it is still NATed on the uplink. Not built: DHCPv6 prefix
  delegation from the uplink. Travel networks almost never delegate, and
  with the VPN on, addresses from the hotel's prefix couldn't be used in
  the tunnel anyway.
- **Zero-leak login:** Debian's `chromium-headless-shell`, driven over
  `--remote-debugging-pipe` by `internal/browser` (a small CDP client, no
  network port). It runs as the system user `rbbrowser` with a temporary
  profile under `/run/rbd/browser`, deleted afterwards; Chromium's sandbox
  is used when the kernel allows it (rbd retries without it and logs a
  warning otherwise). The firewall's output chain only lets that user
  reach `127.0.0.53:53` and the uplink interfaces. The page is laid out
  for the phone's screen size; pictures are `Page.startScreencast` JPEGs,
  long-polled by the UI; taps and typing become CDP input events. It
  closes when the portal check passes (after having seen the portal), after
  5 minutes without input, or after 30 minutes. It is offered on the
  notice page (`canBrowse`: a portal is detected, or the VPN is on and its
  tunnel isn't up) before login mode, and is public to the hotspot like
  login mode. The image grew by about 300 MB, so the system slots are now
  1536 MB; devices on older images must be flashed once.

## 11c. As built in M8

- **Web UI:** the redesign replaced the bare UI (see 9b). The device's build
  contains only the real provider; `npm run demo` builds the demo router.
- **Readings** (`internal/readings`, `internal/agent/readings.go`): CPU load
  from `/proc/stat`, memory, temperature, the firmware's throttling flags,
  traffic through `br-lan`/`br-guest` and per device (`iw station dump`
  byte counters), sampled with every uplink check (5 s). Storage from the
  A/B slot links and `statfs` of the data partition; Ethernet details from
  sysfs, the networkd lease and `resolvectl`.
- **Per-device controls:** pausing adds the MAC address to a `reject` rule
  ahead of `ct state established` in the forward chain, so a paused device
  loses its open connections too, but still reaches RasBeacon (to see why,
  or to undo it); saved in `config.json`. Disconnect is hostapd's
  `deauthenticate`; "forget address" is dnsmasq's `dhcp_release`
  (`dnsmasq-utils`). rbweb tells rbd the viewer's address
  (`/status?viewer=`), so the list marks "this device".
- **VPN profiles:** `config.json` version 3 keeps `vpn.profiles` and
  `vpn.active`; each profile's keys are in `secrets.json` under its name. A
  version 2 file becomes the profile `wireguard.conf` at start, and both
  files are saved at once so keys and profile never part. Switching
  profiles deletes `wg0` and builds it again; the firewall's egress set
  still says `wg0` meanwhile, so nothing leaks.
- **Uplink buttons:** Disconnect is a 10-minute `disable_network` (Wi-Fi) or
  `networkctl down` (Ethernet), so RasBeacon moves to another saved network
  or the other uplink; Renew is `networkctl renew`; Rescan port is
  `networkctl up` + `reconfigure`, or the DHCP safety check for a client
  port.
- **Stay logged in:** a second cookie with a random token; rbd stores its
  SHA-256 (30 days, at most 20 logins) on the data partition, since rbweb
  can't write to disk and keeps sessions in memory.
- **Speed test:** Cloudflare's own library (`@cloudflare/speedtest`, MIT,
  vendored unchanged in `internal/speedtest/cloudflare/`) runs in the
  headless browser as `rbbrowser`, opened on `speed.cloudflare.com` so its
  requests are same-origin. The firewall keeps that user on the uplink, so
  it measures the uplink. Default measurements without packet loss (which
  needs a WebRTC TURN connection and isn't shown); results are sent to
  Cloudflare as on their page. Only on request; one at a
  time; at most 3 minutes. Ookla's CLI was ruled out: its license doesn't
  allow use on routers.

## 12. Changes to the original requirements
1. **Power:** there is no battery target. RasBeacon runs from a USB-C
   supply or a power bank, whichever the user prefers (the earlier "12 hours on a
   common bank" goal was dropped).
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

## 13. Decisions

- Q1: Apache-2.0.
- Q2: developed privately, then published as a public repository (see 2.1).
- Q4: HTTP by default; HTTPS setting with a generated or uploaded certificate.
- Q6: IPv6 for clients (ULA plus NAT66, section 4.4).
- Q8: manual upload, plus an opt-in GitHub check.
- Q9: SSH off by default, with a setting to turn it on.
- Q10: see Q10b below.
- Q11: Pi 4B, Pi 5 and Zero 2 W all supported. Hotspot adapters: MT7612U
  (reference) and TP-Link AC1300 (RTL8812BU/AU, at risk until the M1 gate).
- Q3: `setup.txt` is optional and checked on every boot until claimed; an
  invalid file never falls back to an open network (section 5).
- Q5: portal mode applies to all client devices (4.6, including why a hole is
  needed at all).
- Q7: WireGuard MTU 1380.
- Q12: Ethernet role setting, *Uplink* (default) or *Client port*, with a
  foreign-DHCP safety check (4.1).
- Q1b: project name **RasBeacon**. It avoids the "Raspberry Pi" trademark in
  the name; the image is described as based on Debian and Raspberry Pi OS
  packages.
- Q10/Q10b: hotspot subnet **`10.87.0.0/24`** (guest `10.87.8.0/24`), with
  fallback to `10.88.0.0/24` if the uplink or tunnel overlaps.

