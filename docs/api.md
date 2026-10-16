# RasBeacon HTTP API (v1)

The web UI, and later the mobile app, talk to RasBeacon only through this
API. It is served by `rbweb` at `http://10.87.0.1/api/v1/` (also
`http://rasbeacon.lan/api/v1/`) and is reachable **only from devices on the
hotspot**, never from the upstream network.

- **Request and response bodies are JSON.** The field names and types are
  defined once in Go ([`internal/api/types.go`](../internal/api/types.go)) and
  mirrored in TypeScript ([`web/src/data/types.ts`](../web/src/data/types.ts)).
  The web UI calls them from one file,
  [`web/src/data/real/api.ts`](../web/src/data/real/api.ts).
- **Errors** return a 4xx/5xx status with an `ErrorResponse`:
  `{ "error": "Must be 8 to 63 characters long.", "field": "hotspotPassword" }`.
  The `error` text is a full sentence you can show to the user. `field`, when
  present, names the input that was rejected.
- **Versioning:** breaking changes get a new prefix (`/api/v2/`). Adding
  fields to responses is not a breaking change, so clients must ignore fields
  they don't know.

## Authentication

1. **Login:** `POST /api/v1/session` with
   `{ "password": "...", "stayLoggedIn": false }`.
2. On success the response sets an HttpOnly cookie named `rasbeacon_session`
   and returns `{ "loggedIn": true, "csrfToken": "..." }`.
3. **Every request that changes something** (POST, PUT, DELETE) must send
   that token in the `X-CSRF-Token` header. Without it the request gets 403.
4. **Sessions end** after 12 hours without use, on logout, when `rbweb`
   restarts, or after a factory reset.
   With `stayLoggedIn: true`, the browser also gets a second HttpOnly cookie,
   `rasbeacon_remember`, valid for 30 days. While it is valid, a browser
   whose session ended (for example because RasBeacon restarted) gets a new
   session without the password. rbd keeps only the token's SHA-256 hash
   (`remembered-logins.json` on the data partition). Logging out forgets it;
   a factory reset forgets all of them.
5. **Rate limiting:** after 5 wrong passwords from one device, each further
   attempt waits twice as long (2 s, 4 s, … up to 5 min). After 20 wrong
   passwords within 5 minutes from any mix of devices, all logins pause for
   5 minutes. A limited request gets 429 with a `Retry-After` header.

*Planned (before the v2 mobile app):* token login for apps, sent as
`Authorization: Bearer <token>`, alongside cookies.

## Routes

### No login needed

| Method and path | Request | Response | Notes |
|---|---|---|---|
| `GET /api/v1/setup` | — | `SetupState` | Mode, claimed or not, and suggestions for the setup wizard |
| `POST /api/v1/setup/claim` | `ClaimRequest` | `ClaimResponse` | Only in `setup` mode (409 otherwise). Also logs the browser in. |
| `GET /api/v1/session` | — | `SessionInfo` | Is this browser logged in? Returns the CSRF token if so. |
| `POST /api/v1/session` | `LoginRequest` (`password`, `stayLoggedIn`) | `SessionInfo` | Login. 401 wrong password, 429 rate-limited. |
| `DELETE /api/v1/session` | — | 204 | Logout |
| `GET /api/v1/captive` | — | `CaptivePortalInfo` | RFC 8908 captive-portal API (`application/captive+json`). `captive` is true during setup. |

### Captive portal login mode (no login needed)

These are public because the notice opens in a phone's own "Log in to
network" sheet, before anyone logs in to RasBeacon. The `POST` routes only
accept a JSON body (`{}`), so other websites can't trigger them.

| Route | Request | Response | Notes |
|---|---|---|---|
| `GET /api/v1/portal` | — | `PortalStatus` | `detected`, `loginUrl`, `canStart`, `loginMode`, `secondsLeft`, `canBrowse`, `browsing` |
| `POST /api/v1/portal/start` | `{}` | `PortalStatus` | Login mode for 10 minutes: devices' web (TCP 80/443) and DNS go directly to the uplink. 409 unless a login page blocks the VPN. |
| `POST /api/v1/portal/extend` | `{}` | `PortalStatus` | Adds 10 minutes, up to 30 in total |
| `POST /api/v1/portal/stop` | `{}` | `PortalStatus` | Ends login mode |
| `POST /api/v1/portal/check` | `{}` | `PortalStatus` | Checks for a login page again within a few seconds ("Check again") |

#### Zero-leak login (RasBeacon's own browser)

The login page opens in a browser on RasBeacon itself; the phone shows
pictures of it and sends taps and typing. Devices' traffic never leaves
the VPN. Public for the same reason as login mode (JSON-only `POST`s).

| Route | Request | Response | Notes |
|---|---|---|---|
| `POST /api/v1/portal/browser/start` | `PortalBrowserStart` (`width`, `height` in CSS pixels) | `PortalStatus` | Opens the login page (`loginUrl`, or a plain-HTTP address that the network redirects). 409 unless `canBrowse`. Takes a few seconds. |
| `GET /api/v1/portal/browser/frame?after=<seq>` | — | `PortalBrowserFrame` | Waits up to 10 s for a picture newer than `seq`, then returns the newest: `image` (base64 JPEG), `width`, `height`, `url`, `title`. `running: false` once the browser closed. |
| `POST /api/v1/portal/browser/input` | `PortalBrowserInput` | 204 | `action`: `click` (`x`, `y` in page pixels), `scroll` (`x`, `y`, `deltaY`), `type` (`text`, at most 1000 characters), `key` (`Enter`, `Backspace`, `Tab`, `Escape`), `back`, `reload`, `navigate` (`url`, http/https only). 409 when no browser is open. |
| `POST /api/v1/portal/browser/stop` | `{}` | `PortalStatus` | Closes it. It also closes by itself when the login worked, after 5 minutes without input, or after 30 minutes. |

`GET /api/v1/setup` also reports `portalNotice: true` while the notice is
shown, and `GET /api/v1/captive` (RFC 8908) then answers
`captive: true` with `user-portal-url` pointing to `/#portal`.

### Login required

| Method and path | Request | Response | Notes |
|---|---|---|---|
| `GET /api/v1/status` | — | `Status` | Hotspot (with `metered`), connected devices, installed `plugins` (`id`, `name`, `state`, `icon`), guest network (`guest`), VPN (with `routedIpv6Prefix` while in use), radios (with `maxAps`), device info, warnings. Also: `throughput` (rates through the hotspot, sampled every 5 s), `system` (CPU, memory, temperature, throttling), `device`, `storage`, `lan` (subnet, DHCP range), `speedtest` (`provider` and the `last` result, or null), and `ethernetDiagnostics` (null unless the cable carries the internet). Each client has `band`, `downMbps`, `upMbps`, `internetAccess`, `pausedSeconds` (while paused), `isThisDevice` (the device viewing the page, told by its address), `name` (the owner's name for it, if any), `downBytes`/`upBytes` (data used since the counts were last reset; Wi-Fi devices only) and `limitReached` (paused by the data limit). Devices behind the Ethernet port (when it is a client port) are listed too, with `wired: true` and an empty `band`; their traffic isn't measured. The uplink has `subnetMask`, `interfaceMac`, Wi-Fi `bssid`/`channel`/`frequencyMhz`, and Ethernet `linkSpeed`/`duplex`, when known. |
| `GET /api/v1/settings/hotspot` | — | `HotspotSettings` | Never includes the password |
| `PUT /api/v1/settings/hotspot` | `HotspotSettings` | `HotspotSettings` | Empty `password` keeps the current one. Restarts the hotspot, unless only `metered` changed. `metered: true` sends Android's metered hint (DHCP option 43 `ANDROID_METERED`); other systems don't read a hint. |
| `GET /api/v1/settings/guest` | — | `GuestSettings` | The guest network: `enabled`, `ssid`, `hasPassword`, `supported` (the adapter can broadcast two networks) |
| `PUT /api/v1/settings/guest` | `GuestSettings` | `GuestSettings` | Empty `password` keeps the saved one. A password is required to turn it on, and must differ from the hotspot's; the name must differ too. 400 with `field: "enabled"` if the adapter can't run it. Restarts the hotspot. |
| `POST /api/v1/settings/hotspot/reveal-password` | `PasswordRequest` (admin password) | `RevealPasswordResponse` | 403 if the admin password is wrong |
| `POST /api/v1/wifi-qr` | `{ network, adminPassword? }` | `{ ssid, svg }` | A QR code phones scan to join a network (the `WIFI:` format), as an SVG image, black on white. `network` is `hotspot` (needs the admin password: 403 if wrong) or `guest` (400 with `field: "network"` while the guest network is off). |
| `GET /api/v1/settings/ssh` | — | `SSHSettings` | |
| `PUT /api/v1/settings/ssh` | `SSHSettings` | `SSHSettings` | Turning SSH on requires at least one valid public key |
| `GET /api/v1/settings/uplink` | — | `UplinkSettings` | Ethernet role and saved upstream Wi-Fi networks (most preferred first). Passwords are never returned; `hasPassword` says one is saved. |
| `PUT /api/v1/settings/uplink` | `UplinkSettings` | `UplinkSettings` | Saves the **whole** list. New networks have `id: ""`. An empty `password` keeps the saved one. Networks left out are forgotten (with their certificates). Errors name the field as `networks.<position>.<input>`, for example `networks.0.password` or `networks.0.enterprise.caCert`. `randomMac: true` joins that network with a random MAC address (wpa_supplicant's `mac_addr=1`), kept until RasBeacon restarts. Enterprise networks (`security: "enterprise"`) carry `enterprise: EnterpriseSettings`: certificates and the key are uploaded as PEM text or base64 DER, are write-only (empty keeps the saved file), and are described by `caCertSummary`, `clientCertSummary` and `hasClientKey`. Saving is refused without a CA certificate and server name unless `allowNoCertificateCheck` is true. |
| `GET /api/v1/settings/dns` | — | `{ servers, withVpn }` | The DNS servers devices' lookups go to when the VPN is off, and with `withVpn` while it is on too. Empty: the uplink network's own. |
| `PUT /api/v1/settings/dns` | `{ servers, withVpn }` | `{ servers, withVpn }` | Up to 4 servers, each an IPv4 or IPv6 address (not loopback, link-local or multicast) or a DNS-over-HTTPS address (`https://dns.google/dns-query`; a bare host name such as `dns.google` means the path `/dns-query`). Answers list the plain addresses first, then the DoH addresses in full. Errors name `servers`. DoH servers are asked through rbdoh (see [architecture.md](architecture.md)); their names are looked up with the plain addresses in the list, then the network's DNS. While the VPN is on, its DNS is used instead, unless `withVpn` is true: then these servers are used and reached through the tunnel (plain addresses get a route through it; rbdoh runs as its own user, whose traffic a rule sends through it), except addresses of plugins (10.89.0.0/24), which stay on RasBeacon. While a hotel login page is in the way, the network's own servers are used, so the login page can load. |
| `GET /api/v1/settings/swap` | — | `{ sizeGb, maxGb }` | The swap file on the SD card (`/persistent/swapfile`): its size in GB (0 = off; 2 by default) and the largest allowed, which leaves 1 GB of the card free. |
| `PUT /api/v1/settings/swap` | `{ sizeGb }` | `{ sizeGb, maxGb }` | Makes the swap file that size (or removes it for 0) and turns it on. Takes up to a minute or two for large sizes. 400 with `field: "sizeGb"` above `maxGb`. |
| `POST /api/v1/uplink/scan` | — | `ScanResponse` | Nearby Wi-Fi networks, strongest first, without RasBeacon's own hotspot. Takes about 5 s. 409 when no radio is free to join Wi-Fi. |
| `POST /api/v1/uplink/disconnect` | — | 204 | Leaves the network that carries the internet now and stays away from it for 10 minutes (Wi-Fi: wpa_supplicant's `disable_network`; Ethernet: the port goes down), so another saved network or the other uplink takes over. Saving the Wi-Fi networks, or "Rescan port", ends it early. 400 when not connected. |
| `POST /api/v1/uplink/renew` | — | 204 | Asks the uplink's DHCP server for the address again (`networkctl renew`). |
| `POST /api/v1/uplink/ethernet/scan` | — | 204 | Checks the Ethernet port again: as an uplink it is brought up and reconfigured; as a client port the DHCP safety check runs again. |
| `POST /api/v1/speedtest` | — | `Speedtest` | Runs Cloudflare's speed test now (about 20 s; stopped after 3 minutes) and answers with the result (`last`: `downloadMbps`, `uploadMbps`, `pingMs`, `server`, `ageSeconds`). It runs Cloudflare's library in RasBeacon's headless browser, which the firewall keeps on the uplink, so it measures the uplink (not the VPN). Results are sent to Cloudflare, as on their own speed test page. Never runs by itself. 409 while one runs; 400 without an internet connection or without the browser. |
| `POST /api/v1/clients/internet-access` | `{ mac, allowed }` | 204 | Pauses (`allowed: false`) or allows a device's internet access. While paused, the firewall forwards nothing from its MAC address; it can still reach RasBeacon. Saved with the settings. |
| `POST /api/v1/clients/disconnect` | `{ mac }` | 204 | Sends the device off the Wi-Fi (hostapd deauthenticates it; it may join again). 400 if it isn't connected, or is on the Ethernet cable. |
| `POST /api/v1/clients/flush-lease` | `{ mac }` | 204 | Makes dnsmasq forget the device's address (`dhcp_release`), so it gets a fresh one next time. 400 if it has no lease. |
| `POST /api/v1/clients/name` | `{ mac, name }` | 204 | Names a device (kept by MAC address in the settings); an empty name removes it. At most 40 characters, no control characters (400, `field: "name"`). |
| `GET /api/v1/settings/usage` | — | `UsageSettings` | `limitMb` (the data limit per device, 0 = none), `since` (when counting started, RFC 3339), `metered` (whether the limit applies now: the hotspot's metered setting) and `devicesPaused`. Data use is counted from the radio's byte counters for each Wi-Fi device and saved every 10 minutes (`usage.json`). |
| `PUT /api/v1/settings/usage` | `{ limitMb }` | `UsageSettings` | 0 to 1000000 (400, `field: "limitMb"`). While the hotspot is metered, a device whose data use (down + up) reaches the limit is paused by the firewall until the counts are reset or the limit is raised. |
| `POST /api/v1/clients/usage/reset` | — | `UsageSettings` | Starts counting again from zero; devices paused by the limit get their internet back. |
| `GET /api/v1/settings/vpn` | — | `VPNSettings` | `enabled`, `killSwitch`, `activeProfile` (the profile in use, or "") and `profiles`. Each `VPNProfile` has a unique `name` (usually the imported file's), the server and address settings, `hasPresharedKey`, `routedIpv6Prefix`, and RasBeacon's own *public* key for it (`publicKey`). Private and preshared keys are never returned. |
| `PUT /api/v1/settings/vpn` | `{ enabled, killSwitch }` | `VPNSettings` | The switches. Turning the VPN on needs a complete profile in use (400, `field: "enabled"`). |
| `POST /api/v1/settings/vpn/import` | `{ conf, name? }` | `VPNSettings` | Reads a WireGuard `.conf` and keeps it as a new profile called `name` (default `wireguard.conf`; `-2`, `-3`, ... is added if taken). The first profile is put in use. Refuses `PostUp`/`PreUp`/`PostDown`/`PreDown`, `Table`, `FwMark`, `SaveConfig`, split tunnels, more than one peer and files without `DNS`. At most 20 profiles. |
| `POST /api/v1/settings/vpn/profile/save` | `VPNProfileDetails` (a `VPNProfile` plus write-only `privateKey`, `presharedKey`, `removePresharedKey`) | `VPNSettings` | Saves one profile's typed-in details; `name` picks it. Empty keys keep the saved ones. Errors name the field (`dns`, `privateKey`, ...) or `conf` for problems with the whole configuration. `routedIpv6Prefix` (optional, e.g. `2001:db8:1234:5678::/64`) is a public /64 the server routes to RasBeacon: devices then get public IPv6 addresses through the tunnel, without NAT; it must be public, exactly /64, and the profile must carry IPv6. |
| `POST /api/v1/settings/vpn/profile/activate` | `{ name }` | `VPNSettings` | Puts another profile in use. The tunnel is rebuilt; the firewall keeps devices' traffic in it meanwhile. |
| `POST /api/v1/settings/vpn/profile/remove` | `{ name }` | `VPNSettings` | Deletes a profile and its keys. If it was in use, the first remaining one takes over; without any, the VPN turns off. |
| `POST /api/v1/settings/vpn/profile/generate-key` | `{ name }` | `VPNSettings` | Makes a new private key for that profile; its new `publicKey` goes to the WireGuard server. |
| `GET /api/v1/settings/https` | — | `HTTPSSettings` | HTTPS on/off and the certificate: `subject`, `fingerprint` (SHA-256), `validUntil`, `uploaded` |
| `PUT /api/v1/settings/https` | `HTTPSSettings` | `HTTPSSettings` | Set `enabled`; `generateNew: true` makes a new certificate; `certificate` + `privateKey` (PEM) upload your own. rbweb restarts about 2 s later. With HTTPS on, login and all routes in this table refuse plain HTTP (403). |
| `POST /api/v1/system/update` | the `.rbupdate` file itself (`application/octet-stream`) | `UpdateStatus` | Checks the signature and board, writes the other slot, checks the SHA-256, then restarts into it on trial. Errors are 400 with `field: "update"` and a readable message. Up to 2 GB, 30 minutes. Refused on the Pi Zero 2 W image (`Status.update.singleSystem`: one system copy, new versions are flashed). |
| `PUT /api/v1/settings/updates` | `{ checkOnline }` | `UpdateStatus` | The optional online check for new releases |
| `POST /api/v1/system/backup` | `BackupRequest` (`adminPassword`, `password` for the file, `includePlugins`) | `{ token, fileName }` | Checks the passwords and answers with a ticket for the download below. 403 for a wrong admin password; 400 with `field: "backupPassword"` for one shorter than 8 characters; 409 while a plugin is being installed or started. |
| `GET /api/v1/system/backup/{token}` | — | the `.rbbackup` file | The backup, streamed as it is made, for the browser to save as it arrives (a link, so a large backup never has to fit in the browser's memory). Each token works once, within 2 minutes. The backup holds the settings, secrets, SSH keys, the HTTPS certificate and Enterprise certificates, and with `includePlugins` the plugins' files, images (`podman save`) and data (a running plugin is paused for the few seconds its data takes to copy on the SD card). Never the device's identity. The file is a gzipped tar archive encrypted with AES-256-GCM in 64 KB chunks under an Argon2id key from the password (see `internal/backup`). If something fails halfway, the download is cut off, so it can't be mistaken for a whole backup. |
| `POST /api/v1/system/restore/upload` | the `.rbbackup` file itself | 204 | Keeps the file until `restore`. 400 with `field: "file"` if it isn't a backup. |
| `POST /api/v1/system/restore` | `RestoreRequest` (`adminPassword`, `password`) | `{ version, created, plugins }` | Opens and checks the whole backup (including its plugins' names, addresses, ports and drives), then replaces the settings (a backup without plugins keeps the installed plugins; one with plugins replaces them) and restarts about 5 s later. 400 with `field: "backupPassword"` for a wrong password or a damaged file, `field: "file"` for a backup from a newer RasBeacon. |
| `POST /api/v1/system/reboot` | — | 204 | Restarts about 5 s later |
| `POST /api/v1/system/factory-reset` | `PasswordRequest` (admin password) | 204 | Erases everything and restarts into setup. Logs everyone out. |
| `GET /api/v1/plugins` | — | `PluginsState` | Installed plugins (see [plugins.md](plugins.md)): each with `state` (`installing`, `waiting` for the VPN, `starting`, `running`, `stopped`, `crashed`, `failed`), `message`, `healthy`, `address`, `permissions` in plain words, `ports` (`key` like `tcp/3000`, `host`, `url` to open), `settings` (passwords only as `hasValue`), `metrics`, `afterInstall`, limits, `updateVersion`/`canUpdate` from the catalog, and `broken` when its files can't be read (then only its ID is known). Also the catalog `sources`, this `board`, `memoryMb`, `swapMb`, `metered`. |
| `POST /api/v1/plugins/upload` | the `.rba` file itself (`application/octet-stream`) | `PluginPreview` | Checks the file (format, manifest, signature, board, memory and swap, the signing key of an installed version, room on the card) and keeps it for 1 hour. 400 with `field: "file"` and the reason. The preview has a `stagingId`, what it may do, suggested ports, its settings, `downloadSize` (a registry image's, asked through the VPN; -1 unknown), `metered` and `warnings`. |
| `POST /api/v1/plugins/catalog` | `{ refresh }` | `PluginCatalog` | The plugins the sources offer. GitHub is asked only with `refresh` (or the first time); otherwise the saved catalog. `errors` by source (no releases, GitHub asking to wait). |
| `POST /api/v1/plugins/catalog/download` | `{ source, file }` | `PluginPreview` | Downloads a catalog plugin through the VPN, then as `upload`. |
| `POST /api/v1/plugins/update` | `{ id }` | `PluginPreview` | Downloads an installed plugin's newer version from the catalog (signed plugins only). |
| `POST /api/v1/plugins/install` | `PluginInstallRequest` (`stagingId`, `ports` by key, `settings` by key, `drive`) | `Plugin` | Installs (or updates) a previewed plugin and answers at once; its `state` shows the progress. Errors name `ports.<key>`, `settings.<key>` or `drive`. An empty password keeps the saved one. |
| `POST /api/v1/plugins/cancel` | `{ stagingId }` | 204 | Forgets a previewed file. |
| `PUT /api/v1/plugins/settings` | `{ id, ports, settings, drive }` | `Plugin` | Saves a plugin's ports, settings and drive; restarts it if it runs. |
| `POST /api/v1/plugins/start`, `POST /api/v1/plugins/stop` | `{ id }` | `PluginsState` | Running or not, kept across restarts. Starting also clears a crash stop. |
| `POST /api/v1/plugins/remove` | `{ id, keepData }` | `PluginsState` | Stops it and deletes it and its image; `keepData` keeps its data for a later install. |
| `POST /api/v1/plugins/logs` | `{ id }` | `{ lines }` | Its last 500 lines of output. |
| `GET /api/v1/plugins/drives` | — | `{ drives }` | USB drives' partitions plugins may use (`uuid`, `label`, `fsType`, `size`, `usedBy`). |
| `PUT /api/v1/plugins/sources` | `{ sources }` | `PluginsState` | The extra catalog sources, `owner/repo` (errors name `sources.<n>`). |
| `GET /api/v1/diagnostics` | — | text file | Troubleshooting report. Passwords are removed and MAC addresses partly hidden. |

## Modes (`SetupState.mode`, `Status.mode`)

| Mode | Meaning | What the UI shows |
|---|---|---|
| `setup` | First boot; waiting for the owner | Setup wizard |
| `setup-closed` | Nobody finished setup within 30 min | "Power-cycle to reopen" notice |
| `setup-error` | `setup.txt` exists but is unusable | "Fix setup.txt" notice |
| `normal` | Setup done, hotspot running | Login, then the main pages |
| `no-radio` | No Wi-Fi radio can run a hotspot | "Plug in an adapter" notice |

## How the pieces connect (for contributors)

```
browser ──HTTP──▶ rbweb (unprivileged) ──HTTP over /run/rbd/api.sock──▶ rbd (root)
```

`rbweb` checks the session and CSRF token, then forwards each
login-required route to the matching route on `rbd`
([`internal/webserver/server.go`](../internal/webserver/server.go), the
`forwarded` table). `rbd`'s own routes are in
[`internal/agent/server.go`](../internal/agent/server.go).
