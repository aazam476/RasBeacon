# RasBeacon HTTP API (v1)

The web UI, and later the mobile app, talk to RasBeacon only through this
API. It is served by `rbweb` at `http://10.87.0.1/api/v1/` (also
`http://rasbeacon.lan/api/v1/`) and is reachable **only from devices on the
hotspot**, never from the upstream network.

- **Request and response bodies are JSON.** The field names and types are
  defined once in Go ([`internal/api/types.go`](../internal/api/types.go)) and
  mirrored in TypeScript ([`web/src/api/types.ts`](../web/src/api/types.ts)).
- **Errors** return a 4xx/5xx status with an `ErrorResponse`:
  `{ "error": "Must be 8 to 63 characters long.", "field": "hotspotPassword" }`.
  The `error` text is a full sentence you can show to the user. `field`, when
  present, names the input that was rejected.
- **Versioning:** breaking changes get a new prefix (`/api/v2/`). Adding
  fields to responses is not a breaking change, so clients must ignore fields
  they don't know.

## Authentication

1. **Login:** `POST /api/v1/session` with `{ "password": "..." }`.
2. On success the response sets an HttpOnly cookie named `rasbeacon_session`
   and returns `{ "loggedIn": true, "csrfToken": "..." }`.
3. **Every request that changes something** (POST, PUT, DELETE) must send
   that token in the `X-CSRF-Token` header. Without it the request gets 403.
4. **Sessions end** after 12 hours without use, on logout, when `rbweb`
   restarts, or after a factory reset.
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
| `POST /api/v1/session` | `PasswordRequest` | `SessionInfo` | Login. 401 wrong password, 429 rate-limited. |
| `DELETE /api/v1/session` | — | 204 | Logout |
| `GET /api/v1/captive` | — | `CaptivePortalInfo` | RFC 8908 captive-portal API (`application/captive+json`). `captive` is true during setup. |

### Login required

| Method and path | Request | Response | Notes |
|---|---|---|---|
| `GET /api/v1/status` | — | `Status` | Hotspot, connected devices, radios, device info, warnings |
| `GET /api/v1/settings/hotspot` | — | `HotspotSettings` | Never includes the password |
| `PUT /api/v1/settings/hotspot` | `HotspotSettings` | `HotspotSettings` | Empty `password` keeps the current one. Restarts the hotspot. |
| `POST /api/v1/settings/hotspot/reveal-password` | `PasswordRequest` (admin password) | `RevealPasswordResponse` | 403 if the admin password is wrong |
| `GET /api/v1/settings/ssh` | — | `SSHSettings` | |
| `PUT /api/v1/settings/ssh` | `SSHSettings` | `SSHSettings` | Turning SSH on requires at least one valid public key |
| `GET /api/v1/settings/uplink` | — | `UplinkSettings` | Ethernet role and saved upstream Wi-Fi networks (most preferred first). Passwords are never returned; `hasPassword` says one is saved. |
| `PUT /api/v1/settings/uplink` | `UplinkSettings` | `UplinkSettings` | Saves the **whole** list. New networks have `id: ""`. An empty `password` keeps the saved one. Networks left out are forgotten (with their certificates). Errors name the field as `networks.<position>.<input>`, for example `networks.0.password` or `networks.0.enterprise.caCert`. Enterprise networks (`security: "enterprise"`) carry `enterprise: EnterpriseSettings`: certificates and the key are uploaded as PEM text or base64 DER, are write-only (empty keeps the saved file), and are described by `caCertSummary`, `clientCertSummary` and `hasClientKey`. Saving is refused without a CA certificate and server name unless `allowNoCertificateCheck` is true. |
| `POST /api/v1/uplink/scan` | — | `ScanResponse` | Nearby Wi-Fi networks, strongest first, without RasBeacon's own hotspot. Takes about 5 s. 409 when no radio is free to join Wi-Fi. |
| `GET /api/v1/settings/vpn` | — | `VPNSettings` | WireGuard settings. Keys are never returned except RasBeacon's own *public* key (`publicKey`). |
| `PUT /api/v1/settings/vpn` | `VPNSettings` | `VPNSettings` | Saves typed-in settings. `privateKey`/`presharedKey` are write-only (empty keeps the saved key; `removePresharedKey` deletes it). Turning the VPN on needs a complete server. Errors name the field (`enabled`, `dns`, `privateKey`, ...) or `conf` for problems with the whole configuration. |
| `POST /api/v1/settings/vpn/import` | `{ conf }` | `VPNSettings` | Reads a WireGuard `.conf`. Refuses `PostUp`/`PreUp`/`PostDown`/`PreDown`, `Table`, `FwMark`, `SaveConfig`, split tunnels, more than one peer and files without `DNS`. Keeps the "use the VPN" and kill switch settings. |
| `POST /api/v1/settings/vpn/generate-key` | — | `VPNSettings` | Makes a new private key; the answer's `publicKey` goes to the WireGuard server. |
| `POST /api/v1/system/reboot` | — | 204 | Restarts about 5 s later |
| `POST /api/v1/system/factory-reset` | `PasswordRequest` (admin password) | 204 | Erases everything and restarts into setup. Logs everyone out. |
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
