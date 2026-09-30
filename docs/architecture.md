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
- **hostapd** turns the USB Wi-Fi adapter into an *access point*.
- **dnsmasq** hands out addresses (*DHCP*) and answers name lookups (*DNS*)
  for devices on the hotspot.
- **nftables** is the Linux firewall.
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
      renamed `ap0`; the built-in radio is kept for the uplink (milestone 2).
   5. It creates the *bridge* `br-lan` with address `10.87.0.1/24`.
   6. It starts **setup mode** (if nobody has claimed the device) or
      **normal mode**.
3. **rbweb starts** and listens on port 80.

## Setup mode and normal mode

| | Setup mode | Normal mode |
|---|---|---|
| Wi-Fi name | `RasBeacon-Setup-XXXX` | Chosen by the owner |
| Password | None, or from `setup.txt` | Chosen by the owner (WPA2/WPA3) |
| DNS | Every name points to 10.87.0.1, so phones open the setup page (*captive portal*) | Local names only (upstream DNS arrives in milestone 2) |
| Ends | When someone finishes setup, or after 30 minutes | — |

Finishing setup (`Claim` in `internal/agent/operations.go`) saves the admin
password *hash* and the hotspot settings, then switches to normal mode 5
seconds later, so the phone receives the "success" page first.

## Where things are stored

| Path | What | Written by |
|---|---|---|
| `/persistent/rasbeacon/config.json` | Non-secret settings | rbd |
| `/persistent/rasbeacon/secrets.json` (mode 0600) | Hotspot password, admin password hash | rbd |
| `/persistent/rasbeacon/identity.json` | Device ID, IPv6 prefix, suggested password | rbd, once |
| `/run/rbd/*.conf`, `firewall.nft` | Generated configs (in memory) | rbd |
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
- **`test/netns`** runs the real firewall rules and dnsmasq inside Linux
  *network namespaces*, with virtual cables instead of Wi-Fi.
- **`rbd -dev`** (`internal/devsim`) simulates a whole device for UI work,
  and `test/e2e` drives the UI in a real browser against it.
