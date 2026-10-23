# RasBeacon architecture

This is a map of how RasBeacon works, for someone reading the code for the
first time. Terms in *italics* are explained in the [glossary](glossary.md).
The reasons behind the big decisions are in [design.md](design.md) and
[decisions/](decisions/).

## The pieces

```
  your phone / laptop
        │  Wi-Fi (hotspot)                                Raspberry Pi
        ▼
 ┌──────────────┐   ┌──────────┐   ┌────────────┐   ┌───────────────┐
 │ USB Wi-Fi    │──▶│ hostapd  │   │  dnsmasq   │   │  nftables     │
 │ adapter ap0  │   │ (hotspot)│   │ (DHCP+DNS) │   │  (firewall)   │
 └──────────────┘   └────▲─────┘   └─────▲──────┘   └──────▲────────┘
                         │ config files  │ and restarts     │
                         └───────────┬───┴──────────────────┘
                                     │
   browser ──HTTP──▶ rbweb ──local API──▶ rbd (root) ──▶ /persistent/rasbeacon
                (unprivileged)           (/run/rbd/api.sock)     (settings)
```

- **rbd** (`cmd/rbd`, logic in `internal/agent`) is the only program that
  changes network settings. It runs as root, keeps the settings, writes the
  config files for hostapd, dnsmasq and nftables, and restarts them.
- **rbweb** (`cmd/rbweb`, logic in `internal/webserver`) serves the web UI
  and the `/api/v1/` API. It runs as the unprivileged `rbweb` user and cannot
  read the settings files. It checks logins, then forwards requests to rbd.
- **hostapd** turns the USB Wi-Fi adapter into an *access point*. It puts
  the hotspot's interface (`ap0`) into the `br-lan` bridge. Every minute rbd
  checks that it is still there and that hostapd still runs, and repairs
  either (`internal/agent/hotspotcheck.go`); the diagnostics report lists
  each repair. The group key isn't changed daily, because some adapters
  and phones miss the change and stop receiving broadcasts (ARP), which
  cuts Wi-Fi devices off while Ethernet ones keep working. The owner's
  allow list and device limit (`internal/agent/access.go`) become
  hostapd's `accept_mac_file` and `max_num_sta`; they only apply to Wi-Fi.
- **dnsmasq** hands out addresses (*DHCP*) and answers name lookups (*DNS*)
  for devices on the hotspot.
- **rbdoh** (`cmd/rbdoh`, logic in `internal/doh`) is the
  *DNS-over-HTTPS* forwarder. It runs only while DoH servers are chosen on
  the Internet page and in use: dnsmasq passes devices' lookups to it on
  127.0.0.1:5053, and it asks the DoH servers over HTTPS. It looks up the
  DoH servers' own names with the plain DNS servers in the list, then the
  network's (127.0.0.53), and keeps the last addresses found in case those
  fail later. It runs as a throwaway unprivileged user (systemd
  `DynamicUser`).
- **nftables** is the Linux firewall. It also does the *NAT* that lets all
  your devices share RasBeacon's one address on the upstream network.
- **wpa_supplicant** joins upstream Wi-Fi networks with the built-in radio.
- **WireGuard** (`wg0`, set up with the `wg` and `ip` tools) is the VPN
  tunnel. rbd reads the `.conf` file itself (`internal/wireguard`) and never
  runs wg-quick.
- **systemd-networkd** gets addresses on the uplinks (Ethernet and Wi-Fi)
  by *DHCP*, and **systemd-resolved** is the device's own DNS resolver
  (127.0.0.53), using the uplink's DNS servers.
- **The web UI** (`web/`) is a React app that runs in your browser and only
  talks to the API.

## What happens at boot

1. **`rasbeacon-firewall-early.service`** loads a *lockdown* firewall
   (`/usr/lib/rasbeacon/lockdown.nft`) before any network interface is up.
   Nothing can reach or pass through the device yet.
2. **rbd starts** (`internal/agent/agent.go`, `Start`):
   1. If `factory-reset` exists on the SD card's `BOOTCONFIG` partition, it
      erases the settings.
   2. It loads the settings. On first boot it creates them, generating a
      unique device ID, a random suggested Wi-Fi password and an IPv6 prefix
      (`internal/identity`).
   3. It replaces the lockdown firewall with the full rules
      (`internal/render/templates/firewall.nft.tmpl`).
   4. It finds the Wi-Fi radios (`internal/radio`), waiting up to 20 s for a
      USB adapter to appear. The USB adapter becomes the hotspot and is
      renamed `ap0`; the built-in radio joins upstream Wi-Fi (the uplink).
   5. It creates the *bridge* `br-lan` with address `10.87.0.1/24` and a
      private IPv6 address.
   6. It switches on *forwarding* in the kernel (only now, with the firewall
      in place).
   7. It starts **setup mode** (if nobody has claimed the device) or
      **normal mode**, and writes the uplink settings for networkd and
      wpa_supplicant (`internal/agent/uplink.go`).
3. **rbd's uplink monitor** checks the connection every 5 seconds (see
   below).
4. **rbweb starts** and listens on port 80.

## Setup mode and normal mode

| | Setup mode | Normal mode |
|---|---|---|
| Wi-Fi name | `RasBeacon-Setup-XXXX` | Chosen by the owner |
| Password | None, or from `setup.txt` | Chosen by the owner (WPA2/WPA3) |
| DNS | Every name points to 10.87.0.1, so phones open the setup page (*captive portal*) | Passed to the device's resolver, which asks the uplink's DNS servers |
| Internet for devices | None (nothing is forwarded) | Through the uplink, with NAT |
| Ends | When someone finishes setup, or after 30 minutes | — |

Finishing setup (`Claim` in `internal/agent/operations.go`) saves the admin
password *hash* and the hotspot settings, then switches to normal mode 5
seconds later, so the phone receives the "success" page first.

## The uplink (internet connection)

- **Which uplink:** Ethernet (when set to *Uplink* and a cable is plugged
  in) wins over Wi-Fi, because its routes have a lower *metric*. Among
  saved Wi-Fi networks, wpa_supplicant joins the first one in your list
  that is in range.
- **Every 5 seconds** (`checkUplink`), rbd:
  1. runs the Ethernet *client-port* safety check when a cable was plugged
     in: it sends one DHCP request (`internal/dhcpprobe`), and only connects
     the port to the hotspot network if no DHCP server answers;
  2. reads the routes, addresses and Wi-Fi state (`internal/uplink` parses
     the output of `ip` and `wpa_cli`);
  3. moves the hotspot to another address range (10.88.0.0/24, ...) if the
     upstream network uses its range too (large campus networks can), and
     restarts the hotspot so devices reconnect and get addresses in the new
     range. It moves only when it must; it returns to 10.87.0.0/24 at the
     next restart;
  4. tells devices whether IPv6 internet is available, through dnsmasq's
     *router advertisements*.
- **WPA-Enterprise (such as eduroam):** the username, password and
  uploaded certificates go into `wpa_supplicant.conf`
  (`internal/agent/enterprise.go`). With an uploaded CA, RasBeacon only
  accepts a login server whose certificate it signed, carrying the server
  name if one is given; with a server name alone, one signed by a CA in the
  system's list. With neither, it trusts the server's certificate at the
  first connection: wpa_supplicant reports its SHA-256 on the control
  socket (`internal/wpaevents`), rbd saves it (`servercert.go`), and from
  then on only that certificate is accepted (`hash://server/sha256/...`).
  See [wpa-enterprise.md](wpa-enterprise.md).
- **IPv6:** devices always get a private IPv6 address on the hotspot. They
  only get an IPv6 route to the internet when the uplink has IPv6; then
  their traffic is translated like IPv4 (*NAT66*).

## The VPN and the kill switch

When the VPN is on, three independent things keep devices' traffic inside
the tunnel (`internal/agent/vpn.go`):

1. **Routing** (`internal/routing`): traffic from the hotspot is looked up
   in table 51820, which only has "into wg0" and "unreachable".
2. **Firewall**: devices' traffic may only be forwarded to `wg0`.
3. **DNS**: dnsmasq asks only the VPN's DNS servers; they are routed into
   `wg0`, and an output rule blocks them everywhere else.

Every 5 seconds a small state machine (`internal/tunnel`) looks at the
tunnel's last handshake and decides between *connecting*, *up*, *down* and
*direct*. Only *direct* (kill switch off, tunnel broken for 30 s) switches
all three to the uplink; before the first check, traffic stays in the
tunnel. `test/netns/vpn_test.go` checks this against a real WireGuard
server, including deleting `wg0` while devices are using it.

The VPN keeps several WireGuard profiles (imported files) and uses one;
switching rebuilds `wg0` while the firewall keeps traffic in it. With the
kill switch on and the VPN turned off, the firewall forwards nothing at
all (the state is *blocked*, with a warning on the VPN and Status pages):
the kill switch means devices never use the internet outside the VPN.

The clock matters for WireGuard and certificates, and the Pi has no clock
battery: `internal/agent/clock.go` saves the time regularly, never lets it
go backwards across a reboot, and falls back to HTTPS servers' `Date`
headers when NTP is blocked (`internal/clock`).

## Hotel login pages (captive portals)

Many hotel and café networks show a login page before letting anyone
online, and block everything else, including the VPN. With the kill switch
on, devices can't reach that page either (`internal/agent/portal.go`):

1. rbd checks for a login page over the uplink, like phones do
   (`internal/portal`).
2. If one blocks the VPN, RasBeacon answers phones' own connectivity checks
   with its notice page (`#portal`), so the phone's "Log in to network"
   sheet opens on it.
3. **Zero-leak login** (recommended, `portalbrowser.go`): rbd opens the
   login page in its own headless Chromium (`internal/browser`, controlled
   over a pipe), running as the unprivileged user `rbbrowser`, which the
   firewall only lets reach the uplink. The notice page shows pictures of
   the page and sends taps and typing back. Devices stay in the tunnel.
   The browser closes once the login worked, or after 5 idle minutes.
4. Or **login mode**: web and DNS traffic (only) goes directly to the
   network for 10 minutes. It ends by itself as soon as the login works,
   and the firewall cuts any connection still on the old path, so nothing
   opened during login mode keeps going outside the VPN.

## Device controls and the speed test

The Clients page can pause a device (the firewall forwards nothing from its
MAC address until it's allowed again), disconnect it (hostapd), or make
dnsmasq forget its address (`internal/agent/clients.go`). The Internet
page's speed test (`internal/speedtest`) runs Cloudflare's own library in
the same headless browser as the zero-leak login, as the `rbbrowser` user
the firewall keeps on the uplink. It runs only when asked.

## The guest network

An optional second Wi-Fi network (`internal/agent/guest.go`): a second
access point (`guest0`) in the same hostapd, on its own bridge `br-guest`
(10.87.1.0/24). Guests' traffic takes exactly the same path as yours (the
VPN, kill switch included), but the firewall lets them reach only DHCP and
DNS on RasBeacon, nothing on `br-lan`, and hostapd keeps guests apart from
each other. It needs an adapter that can run two access points at once
(`radio.MaxAPInterfaces`).

## Updates

Updates use the two copies ("slots", A and B) of the system on the SD card
(`internal/update`, `internal/agent/update.go`):

1. The owner uploads a `.rbupdate` file on the System page. Its manifest
   is signed with the release key (Ed25519); the public key is built into
   the image. Nothing is written until the signature and board check out,
   and until the version does: never older than the one running, and the
   running one must be at least the manifest's `minFromVersion` (the
   oldest version that can install it; see docs/releasing.md). Too old a
   RasBeacon refuses the file and the System page shows the way instead: a
   backup, the new image flashed, the backup restored.
2. rbd streams the new boot and system partitions into the slot that isn't
   running, then checks the SHA-256 of everything against the signed one.
3. The device restarts into the new slot **on trial** (the bootloader's
   "tryboot"). If rbd runs normally for a minute, with the web UI (rbweb)
   and the hotspot (hostapd) running, the new slot becomes the default; otherwise, the next start (forced after 5 minutes) is the old
   slot again.

Settings live on the shared `/persistent` partition, so they carry over.

The optional online check (every 1, 6, 24 or 168 hours, or on *Check
now*) reads the newest 30 releases from GitHub (drafts and pre-releases
left out) and the small `<name>-<version>.update.json` file (version and
`minFromVersion`) of those newer than the running version. It offers the
newest release that can be installed over the running version: when the
newest one needs a newer starting version (a new signing key, say), the
device moves up one step at a time, and the System page names the version
that comes after the step. Only when no step exists does it say the new
version needs a fresh install, before anyone downloads it. Installing still needs the signed `.rbupdate` file. A new
version is announced once to the owner's ntfy topic or webhook
(`internal/notify`).

## Backups

A backup (`internal/agent/backup.go`, `internal/backup`) is a tar archive,
compressed with zstd (the image's `zstd` program, every core) and
encrypted with AES-256-GCM under an Argon2id key from the owner's
password. It always holds the settings, secrets, certificates and the
plugins' settings and files; the owner chooses whether it holds the
plugins' data and their images. A restore replaces the settings and the
plugins, then restarts. A plugin whose image wasn't in the backup gets it
when it starts (`internal/agent/pluginsfetch.go`): its `.rba` from the
catalog, signed with the plugin's pinned key, or its published image by
digest. A backup from a newer RasBeacon is refused.

## RasBeacon's name

The host name is a setting (`internal/agent/hostname.go`), because the
system partition is read-only: rbd sets it at every start. dnsmasq answers
`<name>.lan` and `rasbeacon.lan`, rbweb treats both as RasBeacon's own
address, and the certificate RasBeacon makes for HTTPS names both.

## HTTPS and the reset button

- **HTTPS** (`internal/agent/https.go`, `internal/tlscert`): rbd makes or
  checks the certificate and gives rbweb a readable copy; rbweb serves
  port 443 when the copy exists. The setup wizard and the login-page notice
  stay on plain HTTP, because phones' login sheets refuse self-signed
  certificates. A certificate RasBeacon makes is valid for 820 days (Apple's
  devices refuse server certificates valid for more than 825), and rbd
  makes a new one 30 days before it runs out, or at the next start for
  0.1's ten-year ones. Browsers then show their warning once more, with
  the new fingerprint. An uploaded certificate is never replaced.
- **Reset button** (`internal/button`): a button on GPIO 17 appears to
  Linux as a key (via the `gpio-key` overlay in `config.txt`); holding it
  10 seconds erases all settings, like the `factory-reset` file.

## Where things are stored

| Path | What | Written by |
|---|---|---|
| `/persistent/rasbeacon/config.json` | Non-secret settings | rbd |
| `/persistent/rasbeacon/secrets.json` (mode 0600) | Hotspot password, admin password hash, upstream Wi-Fi passwords, WireGuard keys (per VPN profile) | rbd |
| `/persistent/rasbeacon/remembered-logins.json` (mode 0600) | Hashes of "Stay logged in" tokens, with their expiry | rbd |
| `/persistent/rasbeacon/clock` | Last known time (the "time floor") | rbd |
| `/persistent/rasbeacon/tls/` | HTTPS certificate and key (key root-only) | rbd |
| `/persistent/rasbeacon/update-pending.json` | An installed update until it's confirmed | rbd |
| `/usr/lib/rasbeacon/update-keys/*.pub`, `/usr/lib/rasbeacon/board` | Update signing keys and the board name, built into the image | the build |
| `/persistent/rasbeacon/certs/` (directory 0700) | Certificates and keys for WPA-Enterprise networks, named after each network's ID | rbd |
| `/persistent/rasbeacon/identity.json` | Device ID, IPv6 prefix, suggested password | rbd, once |
| `/persistent/swapfile` (mode 0600) | Swap space (System → Swap) | rbd |
| `/run/rbd/doh.env` | The DoH servers and their bootstrap servers, for rbdoh (in memory) | rbd |
| `/run/rbd/*.conf`, `firewall.nft` | Generated configs (in memory) | rbd |
| `/run/systemd/network/00-rasbeacon-*.network` | Generated networkd settings (in memory) | rbd |
| `/bootfs/setup.txt`, `/bootfs/factory-reset` | Files the owner can create from a computer | the owner |

Every settings write is crash-safe: write a temporary file, flush it, rename
it over the old one (`internal/config/store.go`). A power cut leaves either
the old or the new file, never half of each.

The system itself is read-only, and there are two copies of it (A and B) for
updates with automatic rollback. The image layout comes from
[rpi-image-gen](https://github.com/raspberrypi/rpi-image-gen)'s
`image-rota`; the definition is in `image/`.

## How a settings change flows

For example, when you change the hotspot band:

1. The browser sends `PUT /api/v1/settings/hotspot` with the CSRF token.
2. rbweb checks the session cookie and CSRF token, then forwards the request
   to rbd's `/settings/hotspot`.
3. rbd validates the settings (`internal/config`) and saves them.
4. rbd renders `hostapd.conf` from its template (`internal/render`) and
   restarts `rasbeacon-hostapd.service`.
5. rbd answers, and rbweb passes the answer back to the browser.

## Testing without a Raspberry Pi

- **Unit tests** check each package, and golden files show the exact
  configs generated.
- **`test/hwsim`** logs in to a simulated WPA-Enterprise network: the
  Linux module `mac80211_hwsim` provides virtual radios, hostapd plays the
  campus network, and wpa_supplicant uses RasBeacon's generated settings.
- **`test/netns`** runs the real firewall rules and dnsmasq inside Linux
  *network namespaces*, with virtual cables instead of Wi-Fi. This includes
  devices' lookups going through rbdoh to a local DNS-over-HTTPS server, and
  a laptop on the Ethernet client port found through the real bridge.
- **`rbd -dev`** (`internal/devsim`) simulates a whole device for UI work
  (with a stand-in for Cloudflare's speed test service), and `test/e2e`
  drives the UI in a real browser against it.
- **`cd web && npm run demo`** runs the UI on a demo router, with no device
  at all.
