# Web UI contract (for changing the UI)

The web UI ([`web/`](../web/)) is a React app in two halves that meet in one
file:

- **Screens** ([`web/src/screens/`](../web/src/screens/),
  [`web/src/shell/`](../web/src/shell/), [`web/src/ui/`](../web/src/ui/)):
  what you see. They never fetch anything. They call `useDevice()`, which
  returns the router's state and every action.
- **Providers** ([`web/src/data/`](../web/src/data/)): where the state and
  actions come from.
  - [`real/`](../web/src/data/real/): the router itself, through its API
    ([api.md](api.md)). This is what ships on the device.
  - [`demo/`](../web/src/data/demo/): a demo router in memory, with
    scenarios and worst-case data, for working on the screens without a
    router (`npm run demo`). It is left out of the device's build.
- **The meeting point** is [`web/src/data/device.ts`](../web/src/data/device.ts):
  `DeviceState` (what screens read) and `DeviceActions` (everything they can
  ask for), with the rules every action follows. The data shapes are in
  [`web/src/data/types.ts`](../web/src/data/types.ts), the same as the API's.

[`web/README.md`](../web/README.md) describes the design (foundations,
layout, motion, accessibility) and the provider rules in detail.

## What you may change freely

- Everything in `screens/`, `shell/` and `ui/`: layout, components, styles,
  icons, help text, the order of things.
- `src/index.css` (the color and type tokens).
- The demo's data and scenarios.

## What must stay the same

1. **The `DeviceState` and `DeviceActions` interfaces.** A screen that needs
   something new asks for it there (and both providers implement it); it
   doesn't fetch or compute router data itself. Each action resolves once
   the router has finished and the state shows the result, and rejects with
   a sentence for the user, or with `FieldErrors` for messages next to inputs
   (only under the keys `device.ts` lists for that action).
2. **The wording and roles the end-to-end test uses** (below). The test
   finds controls as a person or a screen reader would: by role and visible
   name. If you change one, change the test with it
   ([`test/e2e/walkthrough.cjs`](../test/e2e/walkthrough.cjs)).
3. **Every state each screen can be in** must still be shown: loading, "Can't
   load ..." with "Try again", "Last refresh failed", field errors, empty
   lists, busy and "Saved." states.
4. **Hash routes** (`/#/vpn`): rbweb serves the app as static files.
5. **Everything bundled:** no CDN links, no web fonts to download, icons
   inline. The phone may have no internet (a hotel login page in the way).
6. **No new runtime packages** without discussing it: everything shipped is
   code we have to trust and keep updated. Build-time tools are easier.
7. **Phone first.** Inputs at 16px or more so iPhones don't zoom.
8. `npm run build`, `npm run lint`, `npm run format:check`, `npm test` and
   `npm run build:demo` pass.

## How to run the UI while working on it

```sh
cd web && npm install
npm run demo        # the demo router, no device needed; /#/demo lists the scenarios
```

Against a simulated device (real API, nothing on your computer changes):

```sh
make dev            # http://127.0.0.1:8080
# or, for instant reload while editing:
make build && ./build/rbd -dev /tmp/rasbeacon-dev &
./build/rbweb -listen 127.0.0.1:8080 -rbd-socket /tmp/rasbeacon-dev/run/rbd/api.sock &
cd web && npm run dev   # open the address it prints
```

- **To see the setup wizard again,** stop everything, delete
  `/tmp/rasbeacon-dev`, and start again.
- **The zero-leak login and the speed test** need a headless Chromium for
  the simulated device's own browser: `make dev
  DEV_BROWSER=/path/to/chrome-headless-shell` (`npx playwright install
  chromium` in `test/e2e` downloads one, under
  `~/.cache/ms-playwright/chromium_headless_shell-*/`). The speed test then
  runs against a local stand-in for Cloudflare's service. For the login page,
  open <http://127.0.0.1:8099/require-login> once: the simulated network now
  shows a hotel login page. On `/#/portal`, press "Check again", then "Open
  the login page here". Any room number logs in.

## Wording and roles the end-to-end test uses

**Navigation.** Links inside `navigation` "Main navigation", matched exactly:
"Status", "Internet", "VPN", "Clients" and, below 1024 px wide, "More", whose
page (inside `main`) links to "Hotspot", "Guests", "SSH", "HTTPS" and
"System". Every page has a level-1 heading with its name. "Log out" is a
button.

**Dialogs** are `role="dialog"` on the page, never `window.confirm`.

| Screen | What the test uses |
|---|---|
| Setup | Heading "Set up RasBeacon"; "Admin password" (exact), "Type it again"; radios "Traveling" and "At home" (under "Where will you use RasBeacon?"); button "Finish setup"; "The two passwords don't match."; heading "Setup complete" |
| Login | "Admin password", switch-styled checkbox "Stay logged in", button "Log in", "Wrong password.", "Forgot the password?" |
| Status | Heading "Connected devices (N)"; 'Connected to Wi-Fi "<name>"'; meter "Memory"; "Connected: your devices' traffic goes through the VPN."; "Metered connection" with "On"; heading "Guest network" and its name |
| Internet | Buttons "Look for nearby networks", one per joinable nearby network (named after it), "Renew lease", "Run speed test" ("Running…" while a test runs, also after leaving the page and coming back); the add-network sheet's "Username" (exact), "Your password", "Advanced" (collapsed) with "CA certificate file ..." (file input) and "Login server name", "Add to list"; "Trusted at the first connection" for an Enterprise network saved without either; the bar (`region` "Unsaved changes") "You have changes that are not saved yet." with "Save"; "Saved. RasBeacon is joining the networks now"; the saved CA certificate's summary ("... (valid until ..."); the speed test's server ("Simulated city (DEV)" on the simulated device) and "Download"; the "USB Ethernet" heading (only with an adapter) with a row per adapter ("Adapter", or "Adapter <name>" with several; "For devices", "Not used", "Checking…" or "No cable"); a "Port forwarding" link (its value "Off" or "N rules") to `/internet/port-forwarding`, which shows "Outside address", a button per rule named after it, "Add rule", and a dialog with "Name", "Protocol" (select: TCP, UDP, TCP and UDP), "Outside port" (one port or a range like `27015-27030`), "Goes to" (select of the hotspot's devices and "<plugin> (plugin)"), "Inside port" (hidden for a range), the "On" checkbox, the "Also open over IPv6" checkbox (for a device, while the VPN routes an IPv6 network), "Save" and, for a saved rule, "Delete rule" |
| VPN | "WireGuard file" (file input), "Or paste its contents", "Import"; the refusal containing "never runs commands"; "WireGuard file imported"; a radio per profile named after it; switch "Use the VPN" (on by itself after the first import); a "Rename <profile>" button per profile; button "Remove <profile>" and dialog button "Remove"; "Server details (advanced)" (collapsed), "MTU ...", "Save", "Must be a number, or empty for the default.", "Saved."; "Generate new key" and dialog button "Generate" |
| Login page (`/#/portal`, no login) | Heading "This network needs a login"; "Nothing to do"; buttons "Check again", "Open the login page here"; picture `img` named "... login page ..."; "Tap a field on the page, then type here", "Type it", "Enter" |
| Clients | Switch "Internet access for <name>" (the owner's name for it when set); filter radio "Paused (N)"; a "Rename <name>" button on each device (a pencil), opening a dialog with the "Name" text box and "Save"; "Calls itself" (the name the device sends, once renamed); a "Fixed address" button on each hotspot device (its value "Off" or the address), opening a dialog with the "Always use this address" checkbox, the "Address" text box and "Save"; on networks with a welcome page, "Welcome page" ("Accepted" or "Not accepted yet") and an "Ask again" button once accepted; "Limit per device" (a number box, empty = none, with a "Unit" select of `MB` or `GB`; "Enter an amount up to 1,000 GB, or leave it empty for no limit." for a wrong one) with "Save", which goes away once saved |
| Plugins | "No plugins yet"; "Install from a file" (file input); "This is not a RasBeacon plugin (.rba) file." for a file that isn't one |
| HTTPS | Switch "Use HTTPS for these settings pages"; "HTTPS is on."; "SHA-256 fingerprint" |
| Hotspot | "Country" (two-letter code; "Use GB for the United Kingdom." for UK), then "Band" (select, value `5GHz`) and "Channel", which offers only the channels that country allows ("This channel isn't allowed in this country." for one picked before); "New Wi-Fi password (leave empty to keep the current one)"; "Save"; "Must be 8 to 63 characters long."; "Saved." (exact); "Metered connection (for example a phone's hotspot or a paid plan)"; "Show password and QR code", which shows the password as text and a QR code to join; "Who can join": "Allowed devices" (collapsed) with "Only allow these devices", "MAC addresses ...", "Save", "... is not a MAC address ...", and its subtitle "Anyone with the password" or "N devices"; "Device limit" (collapsed) with "At most (devices)", a number box (empty = no limit; "Enter a number from 1 to 231, or leave it empty for no limit." for a wrong one) |
| Guests | "Turn on the guest network", "Guest network name", "Guest password", "Save", "Choose a password for the guest network.", "Saved. The hotspot has restarted."; "Show QR code", which shows an image named "QR code to join <name>"; a "Welcome page" link (value "On"/"Off") to `/guests/welcome`: the "Show a welcome page" checkbox, "Show it on" (select), "Title", "Message", "House rules or terms", "Access code", "Ask again" (select), "Also for wired devices" (not when only on the guest network), "Logo" (file input) with "Remove logo", "HTML file" under "Your own page (advanced)" with "Use the built-in page", "Save" |
| System | Button "Rename RasBeacon" opens a dialog with the "Name" text box and "Save" ("Use only lowercase letters, digits and hyphens."); button "Reboot" opens a dialog with "Reboot" and "Cancel"; button "Shut down" opens one titled "Shut down RasBeacon?"; "Factory reset" (collapsed) holds "Erase everything"; button "Change admin password" opens a dialog titled "Admin password" with "Current password", "New password", "Type it again" and "Change" ("Wrong password." next to the current password when it is wrong; then "Admin password changed."); "Back up settings" (a dialog with "Password for the backup", radios "Settings only", "Plugin data" and "Everything" when plugins are installed, and "Back up", which downloads a `.rbbackup` file) and "Restore from a backup" ("Backup file (.rbbackup)", "The backup's password", "Restore"; then "RasBeacon is restarting with the backup's settings.") |
