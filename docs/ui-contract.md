# Web UI contract (for restyling the UI)

This project ships a **bare** web UI: plain HTML with one small stylesheet.
It works fully, but nothing about its look is final. This document is what
you give to whoever does the visual design, whether a person or another
agent. It says what can change freely and what must stay the same so the
restyled screens drop back in without breaking anything.

## What you may change freely

- **Markup and styling in every `*View.tsx` file** under
  [`web/src/`](../web/src/): layout, HTML elements, CSS classes, icons,
  wording of help text, order of fields.
- **`web/src/styles.css`**: replace it entirely, or split it up.
- **CSS approach:** plain CSS, CSS modules, or any CSS-only tool that
  `npm run build` can handle.

## What must stay the same

1. **Each view's props.** Every view receives all its data and actions
   through props and must not do anything else:
   - no `fetch` or imports from `api/client.ts`
   - no validation rules
   - no timers

   Logic lives in the matching `use<Screen>.ts` hook. If a design needs
   something a view doesn't receive, ask for it to be added to the hook and
   to this contract; don't compute it in the view.
2. **The exported names and types.** For example,
   `export function SetupView(props: SetupViewProps)` and
   `export type SetupViewProps`. `App.tsx` imports them by name.
3. **Accessible labels and the wording the end-to-end test uses** (listed
   under each screen below). The test clicks buttons and fills boxes by
   their visible label, as a person or screen reader would. If you really
   want different wording, change it and tell us, and we'll update the test.
4. **Every state each screen can be in** must still be shown: loading, error
   messages, field errors, empty lists, and "saved" confirmations. The
   tables below list them.
5. **No new runtime packages** without discussing it. The UI ships inside
   the device image, so every package is code we have to trust and keep
   updated. Build-time-only tools are easier to accept.
6. **Phone first.** Most use is from a phone on the hotspot. Inputs need a
   font size of at least 16px so iOS doesn't zoom in on focus.

## The design gallery (no device needed)

`cd web && npm install && npm run gallery` opens `gallery.html`: every
screen in its important states, filled with sample data from
`web/src/gallery/samples.tsx`, each in a phone-sized frame. Pick a state
from the list, or tick *Show all*. It uses the real views and
`styles.css`, so restyled views show up there at once. Buttons do nothing
there, and nothing talks to a device. The gallery is not part of the
app that ships on the device.

When a view gets new props, its samples must be updated (TypeScript
points at them).

## How to run the UI while designing

No Raspberry Pi is needed. From the repository root:

```sh
make dev          # simulated device at http://127.0.0.1:8080
# or, for instant reload while editing:
make build && ./build/rbd -dev /tmp/rasbeacon-dev &
./build/rbweb -listen 127.0.0.1:8080 -rbd-socket /tmp/rasbeacon-dev/run/rbd/api.sock &
cd web && npm run dev   # open the address it prints
```

- **To see the setup wizard again,** stop everything, delete
  `/tmp/rasbeacon-dev`, and start again.
- **To see the login-page notice and the zero-leak login** (`/#portal`):
  start with a headless Chromium, `make dev
  DEV_BROWSER=/path/to/headless_shell` (Playwright's
  `chromium_headless_shell-*/chrome-linux/headless_shell` works; `npx
  playwright install chromium` in `test/e2e` downloads it). Then open
  <http://127.0.0.1:8099/require-login> once: the simulated network now
  shows a hotel login page. On `/#portal`, press "Check again", then
  "Open the login page here". Entering any room number logs in.
- **To see the guest network,** nothing special: the simulated adapter
  can run it.
- **Checks to run before handing work back:** `cd web && npm run typecheck
  && npm run lint && npm test`, then `make e2e` from the repository root.

## Screens

Every screen folder is under `web/src/screens/` (the frame is in
`web/src/app/`). "Props" lists what the view receives. Its full TypeScript
definition is the `…Logic` interface in the hook file.

### Frame: `app/LayoutView.tsx`

Shown around the main pages after login.

| Prop | Type | Meaning |
|---|---|---|
| `page` | `"status" \| "internet" \| "vpn" \| "hotspot" \| "guest" \| "ssh" \| "https" \| "system"` | Current page |
| `setPage` | `(page) => void` | Switch page |
| `children` | React content | The current page |

**Wording the test relies on:** navigation buttons named "Status",
"Internet", "VPN", "Hotspot", "Guests", "SSH", "HTTPS", "System".

### Notice: `screens/Notice/NoticeView.tsx`

Full-page message screens.

| Prop | Type | Meaning |
|---|---|---|
| `kind` | `"loading" \| "unreachable" \| "setup-closed" \| "setup-error" \| "no-radio"` | Which message |
| `detail` | `string?` | Extra text, e.g. the network error |
| `retry` | `(() => void)?` | Show a "Try again" button when given |

The messages themselves are in the file and can be reworded freely.

### Setup wizard: `screens/Setup/`

Shown on first boot. It collects the admin password, the hotspot name and
Wi-Fi password, and the band.

| Prop | Type | Meaning |
|---|---|---|
| `values` | `{ adminPassword, adminPasswordConfirm, hotspotSsid, hotspotPassword, hotspotBand }` | Current form values |
| `setField` | `(field, value) => void` | Change one field |
| `errors` | `{ [field]?: string }` | Message per field; show it next to the field |
| `formError` | `string \| null` | Problem not tied to one field |
| `submitting` | `boolean` | Disable the button, show progress |
| `result` | `{ hotspotSsid, switchInSeconds, adminUrl } \| null` | Set when setup succeeded; show the "join your new network" instructions |
| `submit` | `() => void` | Finish setup |
| `setupNetworkSecured` | `boolean` | False means the setup network is open: warn to finish quickly |
| `minutesLeft` | `number \| null` | Minutes until the setup network closes |

**Wording the test relies on:**
- heading "Set up RasBeacon"
- labels "Admin password" and "Type it again"
- button "Finish setup"
- "Setup complete" after success

### Login: `screens/Login/`

| Prop | Type | Meaning |
|---|---|---|
| `password`, `setPassword` | `string`, setter | The password box |
| `error` | `string \| null` | Wrong password or rate-limit message from the device |
| `submitting` | `boolean` | |
| `submit` | `() => void` | Log in |

**Wording the test relies on:** label "Admin password", button "Log in", and
the text "Forgot the password?" (the factory-reset hint).

### Status: `screens/Status/`

It refreshes itself every 10 seconds.

| Prop | Type | Meaning |
|---|---|---|
| `status` | `Status \| undefined` | See `web/src/api/types.ts`: internet connection (`uplink`), hotspot, clients, radios, device, warnings |
| `error` | `string \| null` | Last refresh failed (may appear together with old `status`) |
| `loading` | `boolean` | A refresh is in progress |
| `reload` | `() => void` | Refresh now |

**States to design:**
- loading
- error with no data
- error with old data
- zero connected devices
- warnings present, for example "built-in Wi-Fi is running the hotspot"
- internet: not connected, connected by Ethernet, connected by Wi-Fi (with
  signal), Wi-Fi searching or with no saved networks, no Wi-Fi radio free
  (`uplink.wifi.available` false), no Ethernet port

`describeUplink` in `shared/format.ts` gives the one-line summary.

A login-page banner appears at the top when `status.portal.detected`
(with a link to `/#portal`, which says whether RasBeacon's own browser is
already open there).

Also shown (since milestone 7): the guest network (`status.guest`: off, or
name, running, number of guests), the metered setting
(`status.hotspot.metered`), and public IPv6 through the VPN
(`status.vpn.routedIpv6Prefix`).
The VPN section uses `describeVPNState` from `shared/format.ts`; design the
"no internet because of the kill switch" and "using the internet WITHOUT
the VPN" states so they stand out.

**Wording the test relies on:** heading "Status",
"Connected devices (N)", `Connected to Wi-Fi "<name>"`, "Connected: your
devices' traffic goes through the VPN.", heading "Guest network", the
guest network's name, and "On (Android follows it" for the metered
setting.

### Login-page notice: `screens/Portal/`

Shown at `/#portal`, **without logging in** (it opens in the phone's own
"Log in to network" sheet). It is a full page, not inside the frame. It
refreshes every 5 seconds.

| Prop | Type | Meaning |
|---|---|---|
| `status` | `PortalStatus \| undefined` | `detected`, `canStart` (login mode possible), `loginMode`, `secondsLeft`, `canBrowse` (zero-leak login possible), `browsing` (RasBeacon's browser is open) |
| `loadError` | `string \| null` | |
| `loginUrl` | `string` | The network's login page (or a plain-HTTP fallback) |
| `start`, `extend`, `stop` | `() => void` | Login mode. `start` also goes to `loginUrl` after 2 seconds |
| `check` | `() => void` | "Check again": look for a login page again right away |
| `browser` | `PortalBrowserLogic` | The zero-leak login, see below |
| `busy`, `actionError` | | |

The page offers up to two ways to log in, and the **zero-leak login comes
first** (it is the safe one):
- **Zero-leak login** (`status.canBrowse`): `browser.open()` opens the
  login page in RasBeacon's own browser. While `status.browsing`, show
  `PortalBrowserView` instead of everything else.
- **Login mode** (`status.canStart`): explain the bypass clearly: web and
  DNS from all devices skip the VPN until the login works.

**States to design:** loading; both ways offered; only one of them; the
browser open (picture loading, picture shown, an error from the browser);
login mode with countdown; nothing to do. "Check again" is on every state
except the open browser.

**Wording the test relies on:** heading "This network needs a login",
"Nothing to do", buttons "Check again" and "Open the login page here".

#### The zero-leak login: `screens/Portal/PortalBrowserView.tsx`

Pictures of the login page as RasBeacon's browser shows it, about one per
change. The person taps and swipes **on the picture** as if it were the
real page; text is typed into a box below it, because a picture can't open
the phone's keyboard.

| Prop | Type | Meaning |
|---|---|---|
| `frame` | `PortalBrowserFrame \| undefined` | `title`, `url`, `width`, `height` (the page's size in CSS pixels) |
| `imageSrc` | `string` | The picture for an `<img>` (`""` until the first one) |
| `pointerDown`, `pointerUp` | `(spot) => void`, `(spot, box) => void` | Pass `{ clientX, clientY }` from pointer events, and the `<img>`'s `getBoundingClientRect()` on pointer up. The hook decides tap or swipe and converts the position (`browserGestures.ts`). |
| `wheel` | `(spot, box, deltaY) => void` | Mouse wheel over the picture |
| `text`, `setText`, `sendText` | | The typing box; `sendText` types its text into the focused field on the page |
| `pressKey` | `("Enter" \| "Backspace" \| "Tab") => void` | Special keys |
| `back`, `reload`, `close` | `() => void` | |
| `open`, `busy` | | Used by `PortalView` to open the browser |
| `error` | `string \| null` | |

Design constraints: the picture must keep its aspect ratio (any size is
fine, the hook scales taps), must not scroll or zoom the outer page while
the finger is on it (`touch-action: none`), and its accessible name must
contain "login page". Say plainly that the page runs on RasBeacon and that
devices stay offline until the login works.

**Wording the test relies on:** the picture's name containing "login
page", label "Tap a field on the page, then type here", buttons "Type it"
and "Enter".

### VPN: `screens/VPN/`

| Prop | Type | Meaning |
|---|---|---|
| `saved` | `VPNSettings \| undefined` | What is saved: `configured`, `publicKey` (RasBeacon's, to show), `hasPresharedKey` |
| `loadError` | `string \| null` | |
| `importText`, `setImportText` | `string`, setter | The pasted `.conf` text |
| `setImportFile` | `(file \| null) => void` | Reads a chosen `.conf` file into `importText` |
| `importConf`, `importing`, `importError` | | Import the file (the server checks it) |
| `form`, `setField` | `VPNForm`, setter | `enabled`, `killSwitch` (checkboxes); text inputs `endpoint`, `peerPublicKey`, `addresses`, `dns`, `allowedIps` (comma-separated), `persistentKeepalive`, `mtu`, `presharedKey`, `privateKey`, `routedIpv6Prefix` (optional, advanced: a public IPv6 /64 the server routes to RasBeacon) |
| `save`, `saving`, `saveError` | | |
| `errorField` | `string \| null` | Which input the last server error was about |
| `generateKey` | `() => void` | Asks for confirmation itself |
| `message` | `string \| null` | Confirmation after import, save or new key |

**States to design:** loading, not set up yet, set up but off, on with the
kill switch on/off, import error (the server's message names the line),
keys hidden (only the public key is ever shown).

**Wording the test relies on:** label "Or paste its contents", buttons
"Import" and "Save", checkbox "Use the VPN", the messages "never runs
commands" (import refused), "WireGuard file imported" and "Saved."

### Internet (uplink settings): `screens/Uplink/`

Edits are collected on the page and sent together with "Save" (the server
stores the whole list at once).

| Prop | Type | Meaning |
|---|---|---|
| `values` | `UplinkSettings \| undefined` | `ethernetRole`, `networks` (most preferred first), `ethernetPresent`, `wifiUplinkAvailable` |
| `loadError` | `string \| null` | |
| `setEthernetRole` | `(role) => void` | `"uplink"` or `"client-port"`. Only show the choice when `ethernetPresent`. |
| `moveUp`, `moveDown`, `remove` | `(index) => void` | Reorder or remove a saved network |
| `setPassword` | `(index, password) => void` | New password for a saved network ("" keeps it) |
| `networkErrors` | `{ [index]: string }` | Server errors for a network in the list |
| `newNetwork`, `setNewNetworkField` | form values, setter | `ssid`, `security` (`"personal"`/`"enterprise"`/`"open"`), `password`, `hidden`; Enterprise: `method`, `identity`, `anonymousIdentity`, `serverName`, `allowNoCertificateCheck`, and the chosen files `caCert`, `clientCert`, `clientKey` (`{ name } \| null`) |
| `setNewNetworkFiles` | `(field, files: File[]) => void` | For the file inputs `caCert` (several files allowed), `clientCert`, `clientKey` |
| `applyEduroamPreset` | `() => void` | Fills the form for eduroam |
| `newNetworkErrors` | `{ [input]?: string }` | Per-input messages, including `identity`, `caCert`, `serverName`, `clientCert`, `clientKey` |
| `addNetwork` | `() => void` | Adds the form to the list (not saved yet) |
| `scan`, `scanning` | `() => void`, `boolean` | Look for nearby networks (about 5 s) |
| `nearby` | `NearbyNetwork[] \| null` | `null` = no scan yet; `[]` = nothing found |
| `scanError` | `string \| null` | Also used to explain networks that can't be joined yet (eduroam) |
| `pickNearby` | `(network) => void` | Fill the form from a scan result |
| `dirty` | `boolean` | There are unsaved changes |
| `save`, `saving`, `saved`, `saveError` | | |

`describeBand` and `describeSignal` in `shared/format.ts` describe scan
results.

**States to design:** loading, no saved networks, no Wi-Fi radio free
(`wifiUplinkAvailable` false), scanning, nothing found, unsaved changes,
per-network error, saved, the Enterprise form for each login method
(EAP-TLS asks for certificate files instead of a password), and the
"don't check the login server" override, which must keep its warning and
stay tucked away (it is unsafe).

**Wording the test relies on:**
- buttons "Look for nearby networks", "Add to list", "Save", and one button
  per scan result named after the network
- "(not saved yet)" next to added networks
- button "Set up eduroam"; heading/legend "Enterprise login"; labels
  "Username", "Your password", "CA certificate file (choose several if your
  institution lists more than one)", "Login server name"
- "Choose the network's CA certificate file" (error)
- "Saved. RasBeacon is joining the networks now"

### Hotspot settings: `screens/HotspotSettings/`

| Prop | Type | Meaning |
|---|---|---|
| `values` | `HotspotSettings \| undefined` | `ssid, band, channel, security, country, metered, password` (password empty = keep current) |
| `setField` | `(field, value) => void` | Changing `band` resets `channel` to 0 (default) |
| `errors` | `{ [field]?: string }` | Per-field messages |
| `loadError`, `saveError` | `string \| null` | |
| `saving`, `saved` | `boolean` | Show "Saved" after success |
| `save` | `() => void` | Saving restarts the hotspot, so warn that devices reconnect (except when only `metered` changed) |
| `revealAdminPassword`, `setRevealAdminPassword` | `string`, setter | Admin password box for "show current Wi-Fi password" |
| `revealedPassword` | `string \| null` | Show it when set |
| `revealError` | `string \| null` | |
| `reveal` | `() => void` | |

Channel choices per band come from `CHANNELS` in `shared/validation.ts`.

`metered` is a checkbox. Explain that only Android follows it by itself,
and what to set on iPhones, Macs and Windows (Low Data Mode, metered
connection).

**Wording the test relies on:**
- labels "Band", "New Wi-Fi password (leave empty to keep the current
  one)" and "Metered connection (for example a phone's hotspot or a paid
  plan)"
- button "Save"
- "Saved." (exactly)

### Guest network: `screens/Guest/`

A second Wi-Fi network for visitors.

| Prop | Type | Meaning |
|---|---|---|
| `values` | `GuestSettings \| undefined` | `enabled`, `ssid`, `password` (empty = keep), `hasPassword`, `supported` (false: this adapter can't run two networks) |
| `setField` | `(field, value) => void` | `"enabled"`, `"ssid"` or `"password"` |
| `errors` | `{ enabled?, ssid?, password? }` | Per-field messages |
| `loadError`, `saveError` | `string \| null` | |
| `saving`, `saved` | `boolean` | |
| `save` | `() => void` | Saving restarts the hotspot |

**States to design:** loading, off, on, not supported by the adapter (the
switch is disabled and the reason shown), first time (a password is
required), saved.

Explain what guests can and can't do: internet (through the VPN if it is
on), but not your devices, each other, or these settings.

**Wording the test relies on:** checkbox "Turn on the guest network",
labels "Guest network name" and "Guest password" (or "New guest password
(leave empty to keep the current one)" once one is saved), "Choose a
password for the guest network.", "Saved. The hotspot has restarted."

### SSH settings: `screens/SSHSettings/`

| Prop | Type | Meaning |
|---|---|---|
| `values` | `{ enabled, authorizedKeys } \| undefined` | |
| `setEnabled`, `setAuthorizedKeys` | setters | |
| `loadError`, `saveError` | `string \| null` | |
| `saving`, `saved` | `boolean` | |
| `save` | `() => void` | |

### HTTPS: `screens/HTTPS/`

| Prop | Type | Meaning |
|---|---|---|
| `settings` | `HTTPSSettings \| undefined` | `enabled`, `uploaded`, `subject`, `fingerprint`, `validUntil` |
| `loadError`, `error`, `message` | `string \| null` | |
| `httpsAddress` | `string` | e.g. `https://10.87.0.1/` |
| `onHTTPS` | `boolean` | This page itself is on HTTPS |
| `setEnabled` | `(on) => void` | Saves at once |
| `generateNew` | `() => void` | Asks for confirmation itself |
| `certificateText`, `setCertificateText`, `keyText`, `setKeyText`, `setFile` | | Upload your own certificate and key |
| `upload`, `busy` | | |

Show the fingerprint prominently: owners compare it with the browser's
warning. **Wording the test relies on:** checkbox "Use HTTPS for these
settings pages", "HTTPS is on.", "SHA-256 fingerprint".

### System: `screens/System/`

| Prop | Type | Meaning |
|---|---|---|
| `version` | `string` | Running version |
| `update` | `UpdateStatus \| undefined` | `board`, `canInstall`, `installing`, `message`, `canCheckOnline`, `checkOnline`, `latestVersion`, `latestUrl` |
| `updateFile`, `setUpdateFile` | `File \| null`, setter | The chosen `.rbupdate` file |
| `installUpdate`, `installing` | | Asks for confirmation itself; uploading takes minutes |
| `setCheckOnline` | `(on) => void` | The optional online check |
| `diagnosticsUrl` | `string` | Link target for "Download diagnostics" (a file download) |
| `message`, `error` | `string \| null` | Result of the last action |
| `reboot` | `() => void` | Asks for confirmation itself |
| `resetPassword`, `setResetPassword` | `string`, setter | Admin password box for factory reset |
| `factoryReset` | `() => void` | Asks for confirmation itself |
| `logout` | `() => void` | |

**Wording the test relies on:** button "Log out".

## When the restyled UI comes back

We will:
1. Type-check it against these props (`npm run typecheck`) and run the
   linter, the unit tests, and the end-to-end browser test (`make e2e`).
2. Check that no view calls the API or re-implements a rule, and that no
   runtime packages were added without discussion.
3. Check it on a phone-sized screen.
