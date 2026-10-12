# RasBeacon UI

React + TypeScript front end for the RasBeacon travel router: first-time setup, the notices, the
hotel login page and every settings page, plus the extras from the mockups
(speed test, VPN profiles, client controls, uplink controls).

It is designed like Apple's Settings app: iOS Settings on phones, macOS System Settings on
computers, in light and dark appearance (following the device). There is one set of screens; the
layout around them changes with the width:

- **Below 1024px**, the phone layout: each page starts with a large title that shrinks into a
  translucent bar as you scroll, the content is in rounded groups of rows, and a floating tab bar
  holds Status, Internet, VPN, Clients and More. More lists Hotspot, Guests, SSH, HTTPS and
  System, each with how it stands ("On", "2 guests", "v1.0.0").
- **1024px and wider**, the desktop layout: a floating sidebar with Status and the other eight
  pages in three groups (Connection, Wi-Fi, Device), and a centered content column. Status and
  Clients flow into two columns from 1280px.

Both layouts use the same addresses, so resizing the window keeps you on the same page:

| Address                                                      | Page                                                                                |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------- |
| `/#/`                                                        | Status                                                                              |
| `/#/internet`                                                | Internet: connection, speed test, Wi-Fi networks to join, adding one, Ethernet port |
| `/#/vpn`                                                     | VPN: tunnel, kill switch, profiles, import, server details                          |
| `/#/clients`                                                 | Clients: devices on the hotspot and the guest network                               |
| `/#/hotspot`, `/#/guests`, `/#/ssh`, `/#/https`, `/#/system` | Those settings                                                                      |
| `/#/more`                                                    | The phone's More page                                                               |
| `/#/portal`                                                  | The hotel login page screen; opens without logging in                               |

The original UI's addresses still work: `/#portal` (where the router sends phones), `/#hotspot`,
`/#guest` and the others (`src/App.tsx`).

```bash
npm install
npm run dev           # http://localhost:5173, against an rbweb on port 8080 (see docs/ui-contract.md)
npm run demo          # the same with the demo router: no device needed
npm run build         # type-check + production build into dist/ (embedded into rbweb)
npm run build:demo    # the demo, built into dist-demo/
npm test              # unit tests (vitest) of the real provider's logic
npm run typecheck
npm run lint          # oxlint
npm run format        # Prettier, 120-character lines (format:check only checks)
```

Stack: React 19, React Router 8, Tailwind CSS 4, Vite 8, TypeScript 6, and no other runtime
library. Nothing is loaded from the internet, which matters behind a hotel login page: the text
uses the device's own font (SF Pro on Apple devices, Segoe UI on Windows, Roboto on Android) and
the icons are inline SVG. The build is about 500 KB of JavaScript (150 KB compressed); React
and the router are in a file of their own, which stays cached from one release to the next.

## Try every screen: the demo

`npm run demo` runs the app on sample data instead of a router (the device's own build has no
demo in it). Open **`/#/demo`** (or tap the
Demo tag at the top) for a list of 62 states: setup, the notices, Status with warnings, every
Wi-Fi, Ethernet and VPN state, the update states, each step of the hotel login page, loading and
error states, and one where every change fails. Each one also opens directly with
`?scenario=<id>#<page>`, for example `http://localhost:5173/?scenario=vpn-direct#/` (with `npm run demo`).

The buttons work against an in-memory router with short delays, so busy states, errors and
confirmations can be tried: the password `wrong` is refused, invalid inputs get the router's
messages (`src/data/demo/rules.ts`), a WireGuard file with a `PostUp` line is refused, and the
Enter button under the login page picture "logs in".

**Stress test.** Three of the states feed the screens the hardest data the router can send
(`src/data/demo/worstCase.ts`): 32-byte network names with and without places to break, emoji
and accented names, Chinese and Vietnamese device names, a one-letter name, a device without a
name or an address, 16 devices, five warnings at once, an eduroam login with long certificates,
a 400-day uptime, 1.3 TB of traffic and a 1235 ms ping ("Worst case"); no devices at all
("Empty"); and exactly one of everything ("One"). While one of them is open, a switcher at the
bottom of the screen flips the page you are on between Demo data, Worst case, Empty and One.

## Where things live

```
src/
  index.css          Design tokens (colors for light and dark, type scale, materials), rows, the switch
  main.tsx           Mounts the app
  App.tsx            Routes, the phase gate (setup, login, pages), phone or desktop layout
  data/
    types.ts         Data shapes: the router API's types
    device.ts        useDevice(): the state, and every action with its contract
    errors.ts        FieldErrors: an error with messages for single inputs
    constants.ts     Channels per band, the fallback login page address
    real/            The router itself (what ships on the device)
      RealDeviceProvider.tsx   Every action, through the API; loading and refreshing
      api.ts                   One function per API route (docs/api.md), CSRF token, time limits
      logic.ts                 Pure logic: the phase, input checks, files, gestures, error mapping
      logic.test.ts            Its unit tests
    demo/            The demo router, for `npm run demo` (only App.tsx imports it)
      DemoDeviceProvider.tsx   Demo implementation of every action
      mockData.ts              One consistent sample router
      scenarios.ts             The demo states listed on /#/demo
      worstCase.ts             The stress-test data
      rules.ts                 Stand-ins for the router's input checks
      DemoIndex.tsx, DemoTag.tsx, StressToggle.tsx, loginPagePicture.ts
  lib/               Helpers: class names, formatting, springs, useAction, useForm, useCountdown,
                     useMediaQuery, useScrollEdge
  ui/                The design system: Section and its rows, Switch, Button, Sheet, AlertDialog,
                     ErrorToast, Notice, Badge, Meter, StatRow, ChoiceList, Disclosure, fields, icons
  shell/             Around the pages: PhoneLayout (title bar, tab bar), DesktopLayout (sidebar),
                     FullPage (setup, login, notices, hotel login page), Loaded, More, navigation
  screens/           One folder per page (status, internet, vpn, clients, hotspot, guests, ssh,
                     https, system, portal), the setup/login/notice phases, and common/ for
                     descriptions of states shared by several pages (describe.ts)
```

The screens are the same components on phones and computers; the few differences (a sheet's
buttons, the Status page's readings beside the state) are decided inside them.

## The real router: `src/data/real/`

Screens never fetch anything. They call `useDevice()`, which returns the state (`DeviceState`)
and every action (`DeviceActions`), both in `src/data/device.ts`. Every button and switch calls one
of those actions, so `DeviceActions` is the complete list, and its comments say what each one
does. Two providers implement it: `RealDeviceProvider` (the router, through its API; what ships
on the device) and `DemoDeviceProvider` (`npm run demo`). `App.tsx` picks one by the build mode.

### 1. The provider

- `RealDeviceProvider.tsx` holds the state, loads and refreshes it, and implements every action.
  It sits inside `<HashRouter>` and uses `useLocation()` to load what the open page needs.
- `api.ts` has one function per API route (`docs/api.md`), the CSRF token, `ApiError` and a time
  limit on every request.
- `logic.ts` is the logic from the original UI, kept pure and unit-tested (`logic.test.ts`):
  `decidePhase`, the input checks, reading the add-network form's files (`fileToUpload`,
  `certificatesToUpload`), the VPN details form (`splitList`, `vpnDetailsRequest`), taps and
  swipes on the login page picture (`classifyGesture`, `pageSizeFor`), and turning the router's
  refusals into `FieldErrors` (`screenError`, `networkErrorKey`).

### 2. The phase

`phase` decides what fills the screen. `decidePhase` in the original `app/useApp.ts` computes it
from GET `/setup` and GET `/session`; its `main` is called `ready` here. `portal` is not a phase
here: the `/portal` route shows the login page screen whenever the phase is `login` or `ready`
(setup is done), and otherwise what the phase shows.

- `setup` holds the answer of GET `/setup`. The setup screen reads the suggested network name
  and password, `setupClosesInSeconds` and `setupNetworkSecured` from it (the "Setup complete"
  screen takes the address from the claim's answer, `adminUrl`); the HTTPS page reads
  `lanAddress`.
- `retry` loads `/setup` and `/session` again ("Try again" on "Can't reach RasBeacon").
- After `logIn`, the phase is `ready`; after `logOut`, `login`; a request answered with 401 also
  means `login`. After `factoryReset`, `setup` (once the router answers again).
- **`finishSetup` must not change the phase.** The "Setup complete" screen, with "Join <network>,
  then open <address>" and the countdown, is drawn from its answer while the phase stays `setup`.
  The phone then moves to the new network and opens the address, which starts a fresh page.

### 3. Data and refreshing

- Each `Loadable<T>` holds `data` (undefined until the first answer) and `error` (the last
  request's message, or null). An error with older data shows "Last refresh failed: …" above the
  data; an error without data shows "Can't load …" with "Try again", which calls `reload(key)`
  (`refreshStatus` for the status).
- `status`: GET `/status` every 10 seconds while logged in, and again right after actions that
  change it. Most pages read it, and the desktop sidebar shows the router's name from it.
- Settings (`hotspotSettings`, `guestSettings`, `uplinkSettings`, `vpnSettings`, `sshSettings`,
  `httpsSettings`): load them all once the phase is `ready`, load each again when its page opens,
  and after a save store the PUT's answer (the API answers with the saved settings). Other pages
  read them too: Status needs `vpnSettings` (its "Use the VPN" switch is greyed out until the
  active profile is known), and the phone's More page shows `sshSettings` and `httpsSettings`
  as "On" or "Off". Forms keep their unsaved edits when the data reloads.
- `portal`: GET `/portal` every 5 seconds while `/#/portal` is open, logged in or not. After
  `checkPortal`, load it once more about 6 seconds later, as the original does.
- `portalBrowser`: while `portal.data.browsing`, long-poll GET `/portal/browser/frame?after=<seq>`
  and keep the newest frame; `imageSrc` is `data:image/jpeg;base64,${frame.image}`. A frame with
  `running: false` means the browser closed: set `frame` to undefined and `imageSrc` to "".
- `nearbyNetworks`: null before the first scan, then the `networks` of POST `/uplink/scan`.
- `diagnosticsUrl`: `/api/v1/diagnostics`.
- Numbers come raw (dBm, Mbps, bytes, seconds); the screens round and format them
  (`src/lib/format.ts`). Times are seconds (`ageSeconds`, `connectedSeconds`): count them up
  between refreshes if they should tick.

### 4. What every action must do

- **Resolve when the router has finished and the state shows the result.** Switches and option
  lists move only when the state changes, sheets and alerts close when their action resolves, and
  forms show "Saved." then. An action that resolves without updating the state makes its switch
  spring back.
- **Reject with an `Error` to report a failure**, with a short sentence for the user ("Wrong
  password."). Forms, sheets and alerts show it in place. Switches, option lists and buttons
  outside forms have no room for it, so it appears in a message that drops in at the top of the
  screen on phones and rises in the bottom corner on computers (the error toast,
  `src/ui/ErrorToast.tsx`). The demo's "every change fails" state shows both.
- **Reject with `FieldErrors(fields, message?)`** (`src/data/errors.ts`) to put messages next to
  inputs; `message` is shown for the whole form. Use only the keys `device.ts` lists for that
  action: a message under any other key is shown nowhere. The router's field names need mapping,
  as the original UI does: `hotspot.ssid` becomes `ssid`, and `networkErrorField` turns
  `networks.2.password` into the list position (key `networks.2`). Anything else is a plain
  `Error`.
- **The loading actions** (`retry`, `reload`, `refreshStatus`) resolve either way: a failure goes
  into the state (`phase`, the `Loadable`'s `error`), where the screens show it.
- **Always settle, and use timeouts.** A sheet or alert can't be closed while its action runs, so
  a request that never answers would leave the user stuck. Long operations resolve when they are
  really done: `reboot` when the router answers again.
- **Update the state with functions** (`setState((current) => …)`). A control ignores repeat
  presses while its own action runs, but different controls can run actions at the same time.
- Busy states (spinners, "Saving…", locked dialogs) come from `useAction` and `useForm` in
  `src/lib/`; the provider doesn't have to do anything for them.

### 5. Action by action

API paths are under `/api/v1` and are the original client's functions.

| Action                                                            | Request                                                                        | Notes                                                                                                                                                          |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `retry`, `reload(key)`                                            | the GETs above                                                                 |                                                                                                                                                                |
| `finishSetup(form)`                                               | POST `/setup/claim`                                                            | Check like `validateSetupForm`; answer with the `ClaimResponse`. Don't change the phase.                                                                       |
| `logIn(password, stayLoggedIn)`                                   | POST `/session`                                                                | `stayLoggedIn` is new (see 6).                                                                                                                                 |
| `logOut`                                                          | DELETE `/session`                                                              |                                                                                                                                                                |
| `checkPortal`                                                     | POST `/portal/check`                                                           | Reload `portal` now and after ~6 s.                                                                                                                            |
| `startLoginMode`                                                  | POST `/portal/start`                                                           | Then, after 2 seconds, `window.location.assign(loginUrl \|\| FALLBACK_LOGIN_URL)`, as the original does (the screen also shows an "Open the login page" link). |
| `extendLoginMode`, `stopLoginMode`                                | POST `/portal/extend`, `/portal/stop`                                          |                                                                                                                                                                |
| `openPortalBrowser`                                               | POST `/portal/browser/start`                                                   | Body: `pageSizeFor(innerWidth, innerHeight)`. Then reload `portal`.                                                                                            |
| `closePortalBrowser`                                              | POST `/portal/browser/stop`                                                    |                                                                                                                                                                |
| `portalPointerDown`, `portalPointerUp`                            | POST `/portal/browser/input`                                                   | Keep the down spot; on up, `classifyGesture(down, up, box, frame)` gives a `click` or a `scroll`.                                                              |
| `portalWheel`                                                     | same, `scroll`                                                                 | Position from `classifyGesture(spot, spot, box, frame)`.                                                                                                       |
| `portalType(text)`, `portalKey(key)`                              | same, `type` / `key`                                                           | Delete is `Backspace`, Next field is `Tab`. The screen empties its text box itself.                                                                            |
| `portalBack`, `portalReload`                                      | same, `back` / `reload`                                                        | These don't return promises: put failures in `portalBrowser.error`.                                                                                            |
| `refreshStatus`                                                   | GET `/status`                                                                  |                                                                                                                                                                |
| `setEthernetRole(role)`                                           | PUT `/settings/uplink`                                                         | The saved settings with the new role: it applies at once (the screen asks first), while unsaved list edits stay on the page.                                   |
| `scanNearbyNetworks`                                              | POST `/uplink/scan`                                                            |                                                                                                                                                                |
| `prepareNetwork(form, list)`                                      | none                                                                           | Check with `checkNewNetwork` and answer `toSavedNetwork(…)`, with the chosen files read by `fileToUpload` / `certificatesToUpload`. Nothing is saved yet.      |
| `saveWifiNetworks(networks)`                                      | PUT `/settings/uplink`                                                         | With the current `ethernetRole`. A refused network: `FieldErrors({ 'networks.<index>': message })` (see `networkErrorField`).                                  |
| `setVpnEnabled`, `setKillSwitch`                                  | PUT `/settings/vpn`                                                            | `{ enabled, killSwitch }`, with the other one as saved.                                                                                                        |
| `importVpnProfile({ text, fileName })`                            | POST `/settings/vpn/import` (`conf`, `name`)                                   | The screen has read the chosen file. Reject with the router's message for unsafe files (it names the line).                                                    |
| `saveVpnDetails(profile, form)`                                   | POST `/settings/vpn/profile/save`                                              | Built by `vpnDetailsRequest`. Field keys are the form's field names.                                                                                           |
| `generateVpnKey(profile)`                                         | POST `/settings/vpn/profile/generate-key`                                      | The screen has asked for confirmation.                                                                                                                         |
| `setActiveVpnProfile(name)`, `removeVpnProfile(name)`             | POST `/settings/vpn/profile/activate`, `/remove`                               |                                                                                                                                                                |
| `disconnectUplink`, `renewDhcpLease`, `rescanEthernetPort`        | POST `/uplink/disconnect`, `/uplink/renew`, `/uplink/ethernet/scan`            | Then the status is loaded again.                                                                                                                               |
| `runSpeedtest`                                                    | POST `/speedtest`                                                              | About 20 seconds; the answer is the new `status.speedtest`.                                                                                                    |
| `setClientInternetAccess`, `disconnectClient`, `flushClientLease` | POST `/clients/internet-access`, `/clients/disconnect`, `/clients/flush-lease` | Then the status is loaded again.                                                                                                                               |
| `saveHotspotSettings`                                             | PUT `/settings/hotspot`                                                        |                                                                                                                                                                |
| `revealHotspotPassword(adminPassword)`                            | POST `/settings/hotspot/reveal-password`                                       | Answer with `hotspotPassword`.                                                                                                                                 |
| `saveGuestSettings`                                               | PUT `/settings/guest`                                                          |                                                                                                                                                                |
| `saveSshSettings`                                                 | PUT `/settings/ssh`                                                            |                                                                                                                                                                |
| `setHttpsEnabled`                                                 | PUT `/settings/https`                                                          |                                                                                                                                                                |
| `makeNewCertificate`                                              | PUT `/settings/https` with `generateNew: true`                                 | The screen has asked for confirmation.                                                                                                                         |
| `uploadCertificate({ certificate, privateKey })`                  | PUT `/settings/https`                                                          | Send both files' text (`await file.text()`) as `certificate` and `privateKey`.                                                                                 |
| `installUpdate(file)`                                             | POST `/system/update`                                                          | Takes minutes; resolve when the router has accepted the file. `status.update` (installing, message) shows the rest.                                            |
| `setCheckOnline`                                                  | PUT `/settings/updates`                                                        |                                                                                                                                                                |
| `reboot`                                                          | POST `/system/reboot`                                                          | Resolve when the router answers again.                                                                                                                         |
| `factoryReset(adminPassword)`                                     | POST `/system/factory-reset`                                                   | Then the phase is `setup`.                                                                                                                                     |

### 6. Added to the router for this UI

These features of the mockups were added to the router for this UI (see `docs/api.md`):

- `Status.throughput`, `system`, `device`, `storage`, `lan`, `speedtest` and
  `ethernetDiagnostics`; per client `band`, `downMbps`, `upMbps`, `internetAccess`,
  `pausedSeconds` and `isThisDevice` (rbweb tells rbd the viewer's address); the uplink's
  `subnetMask`, `interfaceMac`, Wi-Fi `bssid`/`channel`/`frequencyMhz`, Ethernet `linkSpeed`/
  `duplex`.
- VPN profiles: `VPNSettings.activeProfile` and `profiles`; older settings become the profile
  `wireguard.conf`.
- "Stay logged in": a 30-day login that survives restarts.
- The uplink buttons, the client controls (pausing uses the firewall), and Cloudflare's speed
  test ([github.com/cloudflare/speedtest](https://github.com/cloudflare/speedtest)). The test
  runs the library in RasBeacon's own headless browser, so it measures the uplink, only when the
  owner presses the button. It uses the library's default measurements without packet loss
  (rounds up to 250 MB downloads that stop once a request takes a second, which matters on a
  metered uplink), and sends each result to Cloudflare, as their own speed test page does.

### Please keep

- Hash routes (`/#/vpn`): they work when the router serves the app as static files.
- Everything bundled: no CDN links, no web fonts to download, icons inline.
- `npm run build`, `npm run lint`, `npm run format:check`, `npm test` and `npm run build:demo`
  passing.

## For the end-to-end test

`test/e2e/walkthrough.cjs` drives this UI against a simulated router, by the roles and wording
`docs/ui-contract.md` lists. What to know when writing a step:

- **Navigation** items are links, with the same names, plus "Clients". Other links contain the
  same words ("Hotspot settings"), so match exactly, inside the navigation:

  ```js
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  await nav.getByRole('link', { name: 'Hotspot', exact: true }).click();
  ```

  Below 1024px, Hotspot, Guests, SSH, HTTPS and System are on the More page: click "More" in the
  navigation, then the page's link in `page.getByRole('main')`. Or go straight to a page by its
  address (`/#/hotspot`, or the old `/#hotspot`). Every page has a level-1 heading with its name.

- **Switches that apply at once** have `role="switch"`: "Use the VPN", "Kill switch", "Use HTTPS
  for these settings pages", "Check online for new versions" and each client's "Internet access
  for <name>". Use `getByRole('switch', …)` or `getByLabel(…)` and click; they have no Save. On the
  VPN page, "Save" saves the server details, inside "Server details (advanced)" (click it open
  first).
- **Settings saved with Save** are still checkboxes, drawn as switches: "Turn on the guest
  network", "Turn on SSH", "Metered connection (…)", "Hidden network (…)" and "Don't check the
  login server (unsafe)". That last one is in the add-network sheet's collapsed "Advanced"
  section: click it open first.
- **Internet:** adding a network happens in a sheet that "Add manually", "Set up eduroam" or a
  scan result opens. Only results that can be added are buttons (named after the network); the
  connected one, saved ones and unsupported ones are plain rows. "Add to list" adds the network
  and closes the sheet. "Save" is in a bar that appears at the bottom once the list has changes
  ("You have changes that are not saved yet.", with "Discard changes"). The Ethernet port role is
  a radio choice that asks first ("Switch now") and applies at once.
- **Sheets and confirmations are dialogs on the page** (`role="dialog"`), not `window.confirm`:
  Reboot → "Reboot", Erase everything → "Erase everything", Make a new certificate → "Make new",
  Generate new key → "Generate", Install update → "Install", removing a VPN profile → "Remove",
  the Ethernet role → "Switch now". A dialog leaves the page the moment it closes; what you see
  sliding or fading away afterwards is an inert copy, hidden from the accessibility tree, so
  `getByRole` never finds it. A text locator can still see it for up to half a second: wait for
  such text to be hidden rather than checking that it is gone at once.
- **Collapsed sections** to click open first: "Other way: login mode" on the login page screen
  (when both ways are offered) before "Start login mode", "Factory reset" on System before the
  admin password and "Erase everything", and "Server details (advanced)" on VPN.
- **Log out** is the round button at the top right on phones (named "Log out") and at the foot
  of the sidebar on computers.
- **Wording:** labels are in sentence case ("Erase everything", "Install update", "Add
  manually"), which Playwright's default name matching ignores; `exact: true` does not. Renamed:
  "Restart RasBeacon" is "Reboot", "Show password" (Hotspot) is "Show Wi-Fi password", and "Make
  a new key pair" is "Generate new key".

## Design

The direction is Apple's own: the Settings app's grouped lists, its type and colors, and the
motion of iOS, with Emil Kowalski's craft rules for the details of animation, the habits of
native mobile apps for what makes a web page feel installed, and deliberately broken data for
the worst cases. Nothing came from a component library: every
piece is in `src/ui/`, built on the browser's own elements (`<dialog>`, radio buttons,
checkboxes, links) so that keyboards and screen readers work without extra code.

**Foundations**

- **Type:** the system font with iOS's text styles (Large Title 34, Title 2 and 3, Headline 17
  semibold, Body 17, Subhead 15, Footnote 13), each a single class (`text-body`) that also sets
  leading and tracking, a denser set on computers, and Apple's per-size tracking where the font
  is SF Pro. Sizes are in rem, so everything, icons and tiles included, grows with the browser's
  text size, as with Dynamic Type.
- **Color:** iOS's system colors, darkened or lightened where needed so text keeps 4.5:1
  contrast, with a teal tint from the app icon for links, buttons and selection. Every color is a
  token in `src/index.css` with a light and a dark value; sheets get the lighter "elevated"
  surfaces in dark appearance. One meaning per color: green for working, orange for what needs a
  look, red for problems and for actions that can't be undone.
- **Materials:** the tab bar, sidebar, title bar, toasts and the bar of unsaved changes are
  translucent glass over the content, and turn solid with "Reduce transparency".
- **Icons:** Material Symbols Rounded, the open set closest to SF Symbols, outline in rows and
  filled on the colored tiles, the selected tab and status symbols (`src/ui/icons.ts` has only the
  ones in use).

**Layout**

- Pages are groups of rows, as in Settings: a heading above, an explanation below, a colored
  tile in front of the rows that stand for a place, hairlines inset to where the text starts. A
  row's value sits on the right while it fits and goes under the title, lined up with it, when it
  doesn't.
- The Status page answers the question first ("Protected", "Not protected", "No internet") with
  a large symbol, then what needs doing about it, then a group for each part of RasBeacon. On wide
  screens the internet connection, the device count and the download rate sit beside the answer.
- Settings that apply at once are switches; settings saved together are forms with one Save.
  Adding something opens a sheet; something that can't be undone asks in an alert, whose buttons
  sit side by side when both labels fit and stack otherwise.

**Motion**

- Things that a finger can move are springs (`src/lib/spring.ts`, after Apple's "Designing
  Fluid Interfaces"): critically damped, described by response instead of duration, starting
  from where the element is and at the speed it moves, so any motion can be grabbed or reversed
  midway.
- A sheet follows the finger 1:1, resists past its top (rubber-banding), and decides on release
  by where a flick would carry it: past halfway it goes, otherwise it springs back, overshooting a
  little only after a flick up. Toasts swipe away the same way.
- Entrances settle in with strong ease-out curves; exits are faster than entrances. A press
  shows at once and fades out after release. The tab bar's pill slides between tabs.
- Nothing ever waits for an animation: a closed dialog is gone at once (the page works again and
  gets the focus back) while a copy plays its way out, and a toast that is replaced or swiped
  away doesn't block the next.
- With "Reduce motion", movement becomes short fades.

**Feeling native**

- Full screen on notched phones with safe areas respected, a status bar that matches the page in
  both appearances, the dynamic viewport height, and a home-screen icon.
- No gray flash on tap, no text inflation in landscape, no long-press callouts or text selection
  on controls (addresses, keys and messages stay selectable), fields at 16px on touch screens so
  iPhones don't zoom, hover effects only where there is a mouse, and scroll that doesn't drag the
  page behind a sheet or chain into the browser's pull-to-refresh.
- Sheets rise above the software keyboard, and a sheet's first field only takes the focus by
  itself where there is a mouse or trackpad, so phones don't pop the keyboard up uninvited.

**Accessibility**

- axe-core finds nothing on any page or demo state, in light and dark, at phone and desktop
  widths, with sheets, alerts, field errors and toasts open.
- Every input has a visible label; errors and progress are announced; the focus is always
  visible, stays inside open dialogs and returns to what opened them; Escape and Android's back
  gesture close dialogs (not while their action runs); small controls have 44px touch areas.
- "Reduce motion", "Reduce transparency" and "Increase contrast" are honored.

**Worst-case data** (the Stress test states)

What it turned up, now handled: long values went under their titles but stayed right-aligned
across several lines (they now line up with the title); long names broke in the middle of a word
(names, addresses and file names now break after `_`, `-`, `.`, `:`, `/` or `@`); five warnings
were five boxes (now one group); speed readings wrapped their units on small phones, some and
not others (now all or none, and one per line at large text); eduroam details squeezed into a
narrow column (now stacked on phones); MAC addresses split across lines; alert buttons and the
save bar wrapped their labels (now they stack, or size to their labels); badges and the Status
page's readings pushed the page sideways at large text sizes; an empty Clients page showed
filters with nothing to filter; "Connected devices (0)" repeated "No devices connected"; and a
single saved network showed arrows it couldn't use. At 320px and at 200% text no page scrolls
sideways, and the only text cut short (a network name or host name in a narrow spot on
computers) shows in full on hover.

## Data

The mockups showed different network names, loads, profiles and uptimes for the same device.
`src/data/demo/mockData.ts` merges them into one consistent router, so every screen agrees.
