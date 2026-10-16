# Plugins

Plugins add features to RasBeacon: blocking ads, sharing files, running a
case's fan. Each runs in its own Podman container with its own address, and
can only do what the owner allowed when installing it.

- **For owners:** [Installing and using plugins](#installing-and-using-plugins)
- **For authors:** start from
  [RasBeacon-plugin-template](https://github.com/aazam476/RasBeacon-plugin-template)
  (a working plugin, the build and release tools, and a step-by-step guide);
  this page is the reference: [making a plugin](#making-a-plugin), [the manifest](#the-manifest),
  [signing](#signing), [publishing in a catalog](#publishing-a-catalog)
- **For contributors:** [How it works](#how-it-works)

RasBeacon's own plugins (AdGuard Home, Samba, Argon ONE V2) are in
[aazam476/RasBeacon-plugins](https://github.com/aazam476/RasBeacon-plugins),
the built-in catalog source.

## Installing and using plugins

**More → Plugins** (the sidebar on a computer) lists the installed plugins.
Install more with **Browse plugins** (the catalog) or **Install from a file**
(a `.rba` file).

Before anything is installed, a sheet shows:

- who made it and whether it is **signed** (with the key's ID), or not;
- what it **will be able to do**, in plain words. A plugin can never do
  anything it didn't ask for here;
- the **ports on RasBeacon** where devices on the hotspot reach it (you choose
  them; like Docker's `3000:3001`, the plugin's own port stays the same);
- its **settings**, and the USB drive it uses, if any;
- what it uses: memory and processor (limits it can't pass), storage, and what
  installing downloads, with a warning on a metered connection.

Installing runs in the background; the plugin's page shows the progress. A
plugin that downloads its image waits while the VPN's kill switch blocks the
internet ("Waiting for the VPN").

Each plugin's page has its numbers (up to four), **Open** for its web page,
**Running** (a stopped plugin stays stopped, also after a restart), its
settings (saving restarts it), what it can do, its **log** (the last 500 lines,
kept in memory), details, updates and **Remove**.

What plugins can't do, whatever they ask for:

- change RasBeacon's settings, or read its passwords;
- be reached by guests, or from the internet or the network RasBeacon joined;
- reach RasBeacon's settings pages (only its DNS server, with the internet
  permission);
- use more memory, processor or storage than they were given.

**After installing:** a plugin may tell you what to do next, for example
AdGuard Home asks you to set the DNS servers (Internet → DNS servers) to its
address and turn on **Also use while the VPN is on**. RasBeacon never makes
such changes for a plugin.

**Crashes:** a plugin that stops by itself is started again. After 5 crashes
in a row RasBeacon stops it and waits for you to start it.

**Updates** never install by themselves. Once a plugin from the catalog is
installed, RasBeacon checks the catalog once a day, and the plugin's page
offers **Update to …**. Updates keep the plugin's settings and data.

**Removing** deletes the plugin and its image. **Keep its data** saves its
data, for installing it again later. A factory reset removes all plugins.
A backup (System → Back up settings) can include the plugins with their
images and data.

## Permissions

| Permission | In plain words | What it gives |
|---|---|---|
| `internet` | Use the internet | Connections out, the same way your devices go: through the VPN when it is on, not at all while the kill switch blocks or a hotel login is in the way. Also RasBeacon's DNS server. |
| `lan-access` | Reach your devices | Connections to devices on your hotspot (never to guests). |
| `usb-drive` | Use a USB drive | The drive the owner picks, mounted with `nosuid,nodev,noexec`, at `usbDrive.path`. Only one plugin gets a drive. |
| `i2c` | Use the I²C bus | `/dev/i2c-*` (RasBeacon turns the bus on). |
| `gpio` | Use the GPIO pins | `/dev/gpiochip*`. |
| `power-button` | Shut down or restart RasBeacon | A socket to ask RasBeacon (see [Power](#power)). |
| `temperature` | Read the temperature | Without it, `/sys/class/thermal` and `/sys/class/hwmon` are hidden. |

Without any permission a plugin can still answer devices on its ports and
keep its own data. A manifest asking for anything else is refused.

## Making a plugin

A plugin is a zip file renamed to `.rba`:

```
manifest.json   what the plugin is and needs (below)
icon.png        a square PNG, 128×128 to 1024×1024 (256×256 is best), at most 512 KB
README.md       optional: shown on its page (Markdown: paragraphs, lists,
                **bold**, *italic*, `code`, links; at most 64 KB)
templates/      optional: files filled in from its settings
image.tar       its container image (`docker save` / `podman save`, arm64), or
                nothing when the manifest names an image in a registry
signature       optional: made by `rbsign plugin-sign`
```

Nothing else may be in the file. The image must be for `linux/arm64`.

Build the image, save it and zip it up:

```sh
docker buildx build --platform linux/arm64 -t my-plugin:1.0 --load .
docker save my-plugin:1.0 -o image.tar
zip -X my-plugin.rba manifest.json icon.png README.md image.tar
```

Or name a published image in the manifest (pinned by digest) and leave
`image.tar` out: RasBeacon downloads it through the VPN when installing.

## The manifest

```json
{
  "id": "adguard-home",
  "name": "AdGuard Home",
  "version": "0.107.57-1",
  "description": "Blocks ads and trackers for every device on your hotspot.",
  "author": "RasBeacon",
  "homepage": "https://github.com/AdguardTeam/AdGuardHome",
  "boards": ["pi5", "pi4", "pi3", "zero2w"],
  "minRasBeacon": "0.1.0",
  "image": { "ref": "docker.io/adguard/adguardhome:v0.107.57", "digest": "sha256:…" },
  "permissions": ["internet"],
  "ports": [{ "name": "Web interface", "container": 3000, "open": true }],
  "memoryMb": 256,
  "cpus": 0.5,
  "minMemoryMb": 0,
  "minSwapMb": 0,
  "storageMb": 512,
  "volumes": [
    { "name": "work", "path": "/opt/adguardhome/work" },
    { "name": "conf", "path": "/opt/adguardhome/conf" }
  ],
  "env": { "TZ": "UTC" },
  "settings": [
    { "key": "password", "label": "Admin password", "type": "password", "required": true }
  ],
  "templates": [
    { "file": "AdGuardHome.yaml", "path": "/opt/adguardhome/conf/AdGuardHome.yaml", "once": true }
  ],
  "afterInstall": "Set the DNS servers on the Internet page to {address}.",
  "metrics": [
    { "label": "Queries today", "url": "http://{address}:3000/control/stats", "path": "num_dns_queries",
      "username": "admin", "password": "{setting:password}" }
  ],
  "health": { "url": "http://{address}:3000/" }
}
```

| Field | |
|---|---|
| `id` | 2 to 32 lowercase letters, digits and dashes. Updates must keep it. |
| `name`, `version`, `description`, `author`, `homepage` | Shown to the owner. `name` at most 40 characters, `version` without spaces or `/`, `description` at most 300, `homepage` an `https://` address. |
| `boards` | `pi5`, `pi4`, `pi3` (3B, 3B+), `zero2w`; empty: all. Others refuse it. |
| `minRasBeacon` | The oldest RasBeacon it works with, like `0.2.0` (empty: any). An older RasBeacon shows it in the catalog as *Needs RasBeacon 0.2.0 or newer* and refuses to install it. Set it when the plugin uses something a newer RasBeacon added. |
| `image` | A registry image: `ref` starting with its registry (`docker.io/…`, `ghcr.io/…`) without a digest, plus `digest` (`sha256:…`, the image index or the arm64 image). Leave it out with `image.tar`. |
| `permissions` | See [Permissions](#permissions). |
| `ports` | `name` (at most 40 characters), `container` (its port), `protocol` (`tcp` or `udp`, default `tcp`), `default` (the suggested port on RasBeacon, 1 to 65535; default the container's), `open` (an Open button for a web page), `https`. Ports 22, 53, 67, 80 and 443 are RasBeacon's. |
| `memoryMb`, `cpus` | Limits; default 256 MB and half a core. |
| `minMemoryMb`, `minSwapMb` | Refuse the install on a Pi with less memory, or less swap (the owner is pointed to System → Swap file). |
| `storageMb` | Its data, one fixed-size file on the SD card (default 256 MB), shared by its `volumes`. It grows when a new version asks for more. |
| `volumes` | Folders of its data: `name` and `path` in the container (each path once). They survive updates. |
| `usbDrive` | `path` where the chosen drive appears, `optional`. With the `usb-drive` permission. A chosen drive replaces a volume at the same path. |
| `env`, `command` | Fixed environment variables (one line each), and a command replacing the image's. |
| `settings` | The form RasBeacon draws: `key`, `label`, `type` (`text`, `password`, `number`, `switch`, `select`), `help`, `default`, `required`, `options` (`value`, `label`), `min`, `max`, `unit`, `env` (the environment variable that gets the value). Labels are at most 40 characters, help 300, units 20, option labels 60; option values are distinct. Passwords are kept with RasBeacon's secrets, exactly as typed, and never shown again. Values are one line, at most 1000 characters. |
| `templates` | Files in `templates/` written into a volume before each start (`path` names a file inside a volume), filled in with Go's `text/template`: `{{.Settings.key}}`, `{{.Address}}`, `{{.Host}}`, `{{port 3000}}`, `{{bcrypt .Settings.password}}` (at most 10 times), `{{json .Settings.key}}`, `{{html .Settings.key}}`. A filled-in file is at most 4 MB. `once` writes it only the first time, so changes the plugin makes are kept. RasBeacon stops the plugin before writing, and never writes through a symbolic link the plugin made: if a folder on the path is a link, the plugin doesn't start. |
| `afterInstall` | What the owner should do next, shown after installing and on its page. |
| `metrics` | Up to 4 numbers on its page, read every 30 s from its own HTTP API: `label` (at most 30 characters), `url`, `path` in the JSON answer (`a.b.0`), `divideBy` (another path), `format` (`number`, `percent` for 0 to 1, `bytes`, `ms`, `seconds`, `text`), `scale`, and `username`/`password` for Basic authentication. |
| `health` | A `url` that answers below 500 when it works. |

Metric and health URLs reach only the plugin itself: `http://{address}`, then
optionally `:port` and a path (`http://{address}:3000/control/stats`).
RasBeacon doesn't follow redirects from them.

Placeholders in `afterInstall`, metrics and health: `{address}` (the
plugin's own address, e.g. `10.89.0.2`), `{host}` (RasBeacon's address on the
hotspot), `{port:3000}` (the port on RasBeacon chosen for container port
3000), `{setting:key}`.

### What a running plugin sees

- An address on `10.89.0.0/24`, fixed for as long as it is installed; DNS at
  `10.89.0.1` (with the internet permission).
- Its volumes, its drive, and the devices its permissions give. No privileges,
  `no-new-privileges`, at most 512 processes.
- Its output goes to its log (256 KB, in memory; its page shows the last 500 lines).

### Power

With `power-button`, `RASBEACON_SOCKET` names a Unix socket (only root in
the container may use it). To shut down or restart RasBeacon, send it:

```
POST /power  {"action": "shutdown"}   (or "reboot")
```

## Signing

Signing is optional: anyone can make and install plugins, signed or not. A
signature lets RasBeacon make sure updates come from the same author, the way
Android checks apps.

```sh
rbsign plugin-keygen                        # once: keep the private key secret
RASBEACON_PLUGIN_SIGNING_KEY=… rbsign plugin-sign -in my-plugin.rba -out my-plugin-signed.rba
```

The `signature` file covers every other file in the `.rba` (their SHA-256),
signed with Ed25519. RasBeacon refuses a file whose signature doesn't match.

- The first install of a signed plugin pins its key.
- Updates must be signed by the pinned key. Another key, or no signature, is
  refused; to change keys, remove the plugin (keeping its data, if wanted) and
  install the new one.
- An unsigned plugin can be updated from a file, signed or not. A signed
  update pins its key from then on. Catalog updates are for signed plugins.

## Publishing a catalog

Any GitHub repository can be a source: owners add it under Browse plugins →
Sources as `owner/repo`. RasBeacon looks at its **newest release**: every
`.rba` asset is a plugin. Add a `rasbeacon-catalog.json` asset to describe
them without downloading each one:

```json
{
  "plugins": [
    {
      "file": "adguard-home.rba",
      "id": "adguard-home",
      "name": "AdGuard Home",
      "version": "0.107.57-1",
      "description": "Blocks ads and trackers for every device on your hotspot.",
      "permissions": ["internet"],
      "boards": [],
      "minRasBeacon": "",
      "downloadSize": 21400000,
      "icon": "adguard-home.png"
    }
  ]
}
```

`icon` names a PNG asset of the same release (at most 128 KB).
`minRasBeacon` is copied from the manifest, so the catalog can mark plugins a
RasBeacon is too old for without downloading them.

**Release names.** RasBeacon only reads the newest release and ignores its
name. RasBeacon's own plugins name theirs after the RasBeacon version they're
made for: `v0.1.0`, `v0.1.1`, … for RasBeacon 0.1.
`downloadSize` is what installing downloads besides the `.rba` (a registry
image).

RasBeacon asks GitHub (without an account, through the VPN) only when the
catalog is opened or **Check now** is pressed, and once a day while a plugin
from a catalog is installed. If GitHub asks it to wait, it waits and shows
the catalog it has.

## How it works

`internal/plugin` reads and checks `.rba` files, manifests, signatures and
catalogs; `internal/agent/plugins*.go` installs and runs them.

- **Podman** keeps images on `/persistent/containers` (`/etc/containers/`).
  Its own firewall rules are off: RasBeacon's firewall decides.
- **Network:** the bridge `rbplug0` (`10.89.0.1/24`). The firewall
  (`render.PluginRule`) forwards devices' connections to a plugin's port only
  from the hotspot and only to RasBeacon's address (`dnat`), lets plugins with
  `internet` out like devices (into `wg0` when the VPN is in use; the routing
  rules 102/103 send their traffic to the tunnel table), and lets nothing from
  the uplink in.
- **Downloads** (catalog, `.rba`, images) carry the firewall mark `0x5242`,
  which rule 111 sends into the tunnel; names are looked up with dnsmasq, which
  asks the VPN's DNS. Podman downloads through a proxy in rbd that makes such
  connections.
- **Data:** each plugin's `data.img` (ext4, `fallocate`d) in
  `/persistent/rasbeacon/plugins/installed/<id>/`, loop-mounted `nosuid,nodev`
  under `/run/rbd/plugins/<id>/`.
- **Settings:** `config.json` (`plugins`), passwords in `secrets.json`.
- The diagnostics report lists plugins and their state, nothing more.
