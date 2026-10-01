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
| `page` | `"status" \| "internet" \| "hotspot" \| "ssh" \| "system"` | Current page |
| `setPage` | `(page) => void` | Switch page |
| `children` | React content | The current page |

**Wording the test relies on:** navigation buttons named "Status",
"Internet", "Hotspot", "SSH", "System".

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

**Wording the test relies on:** heading "Status",
"Connected devices (N)" and `Connected to Wi-Fi "<name>"`.

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
| `values` | `HotspotSettings \| undefined` | `ssid, band, channel, security, country, password` (password empty = keep current) |
| `setField` | `(field, value) => void` | Changing `band` resets `channel` to 0 (default) |
| `errors` | `{ [field]?: string }` | Per-field messages |
| `loadError`, `saveError` | `string \| null` | |
| `saving`, `saved` | `boolean` | Show "Saved" after success |
| `save` | `() => void` | Saving restarts the hotspot, so warn that devices reconnect |
| `revealAdminPassword`, `setRevealAdminPassword` | `string`, setter | Admin password box for "show current Wi-Fi password" |
| `revealedPassword` | `string \| null` | Show it when set |
| `revealError` | `string \| null` | |
| `reveal` | `() => void` | |

Channel choices per band come from `CHANNELS` in `shared/validation.ts`.

**Wording the test relies on:**
- labels "Band" and "New Wi-Fi password (leave empty to keep the current
  one)"
- button "Save"
- "Saved. The hotspot has restarted."

### SSH settings: `screens/SSHSettings/`

| Prop | Type | Meaning |
|---|---|---|
| `values` | `{ enabled, authorizedKeys } \| undefined` | |
| `setEnabled`, `setAuthorizedKeys` | setters | |
| `loadError`, `saveError` | `string \| null` | |
| `saving`, `saved` | `boolean` | |
| `save` | `() => void` | |

### System: `screens/System/`

| Prop | Type | Meaning |
|---|---|---|
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
