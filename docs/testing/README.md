# Testing RasBeacon

RasBeacon's tests are in four groups, from fastest to slowest. Each group
has its own folder, and every test file covers one area, so a new test goes
next to the tests of the same area. A file starts with a comment that says
what it covers.

| Group | What it checks | Where | Run it |
| --- | --- | --- | --- |
| [Back end](#back-end) | rbd, rbweb and their packages, with fake commands and files | `internal/*/…_test.go`, `test/netns/`, `test/hwsim/` | `go test ./...` |
| [Front end](#front-end) | the web UI: its logic, the requests it sends, and its screens | `web/test/` | `cd web && npm test` |
| [End to end](#end-to-end) | the whole product in a browser, on a phone-sized and a desktop-sized screen | `test/e2e/` | `make e2e` |
| [Hardware](#hardware) | what only a Raspberry Pi can show: radios, USB adapters, real networks | [full.md](full.md) | by hand |

`make test` runs the first two, `make lint` the formatting and static
checks. CI runs all but the hardware tests on every push and prints how
much of the code the unit tests run (`go test -cover ./...` and
`npm run coverage`).

### When CI runs

CI runs once for each change: on every push to any branch (merging a pull
request is a push to main, so main is checked after each merge), and for
pull requests from forks. A pull request from a branch of this repository
is tested by its branch's push, so its `pull_request` jobs show as
"Skipped"; they count as passing. To make sure the tested commit is the one
that lands, main's branch rules should have "Require branches to be up to
date before merging" on.

### Required checks on main

These jobs should be required before merging into main (GitHub → Settings →
Branches → main → Require status checks to pass; only the repository's
owner can change it):

- Go (format, vet, staticcheck, unit tests)
- Web UI (typecheck, lint, format, tests, build)
- Network namespace tests (real firewall and dnsmasq)
- Simulated Wi-Fi tests (Enterprise logins, guest network)
- Browser end-to-end test (simulated device, phone)
- Browser end-to-end test (simulated device, desktop)

The full-system test joins the list once it has passed about 20 runs in a
row.

## Back end

### Go unit tests

Each package has tests next to its code. They never touch the computer
they run on.

- **rbd** (`internal/agent`): `newTestEnv` gives a fake Raspberry Pi: a
  temporary folder for `/sys`, `/persistent` and `/run`, a `fakeRunner`
  that records each command instead of running it (and answers with
  `outputs`), and fake timers (`e.timers.runAll()`) for things that
  happen later. `newClaimedEnv` is the same after first-time setup,
  `newPluginEnv` with Podman faked too.
  - Area files: `vpn_test.go`, `access_test.go`, `backup_test.go`, …
  - `server_test.go` checks rbd's HTTP routes themselves: every route
    refuses a body it can't read, what a settings page reads can be saved
    back, and the answers of the actions (reboot, factory reset, …).
- **rbweb** (`internal/webserver`): `newTestServer` runs rbweb against a
  fake rbd. `server_test.go` covers logins, sessions and the setup
  redirects; `streams_test.go` the uploads, the backup download and the
  factory reset; `routes_test.go` checks that every route in
  [docs/api.md](../api.md) exists and that every route rbweb forwards is
  documented there.
- **Config files** (`internal/render`) are compared with golden files in
  `internal/render/testdata/`. After changing a template, run
  `go test ./internal/render -update` and read the diff of the `.golden`
  files before committing it.
- Other packages (`config`, `backup`, `plugin`, `update`, …) test their
  functions directly, mostly as tables of cases.

To add a test, find the file of the area (or add `<area>_test.go`), and
name the test after the behaviour:
`TestFactoryReset_NeedsTheAdminPassword`, not `TestReset2`. Prefer a table
when several inputs check the same rule.

Background work (the uplink, portal and clock monitors, the update and
catalog checks, the plugin watcher) is a loop that only sleeps and calls
one round: `checkClock`, `updateCheckDue`, `catalogCheckDue`,
`pluginWatchRound`, `CheckPortal` and so on. Test the round, not the loop;
a new background job should follow the same shape.

What the unit tests don't run: those loops themselves, and code that needs
a real kernel, radio, network or browser (raw DHCP packets, sockets marked
for the VPN, Chromium, Podman). The groups below cover those.

### Network namespace tests (`test/netns/`)

The real firewall (nftables), dnsmasq, WireGuard and DHCP in Linux network
namespaces: a phone, RasBeacon and a hotel network, wired together. Each
file is one area (`vpn_test.go`, `guest_test.go`, `portal_test.go`, …).

```sh
sudo -E go test -count=1 -v ./test/netns/
```

Needs Linux, root, `nftables`, `dnsmasq`, `busybox`, `iproute2` and
`wireguard-tools` (CI's `netns` job lists them). Without root the tests
are skipped; `RASBEACON_REQUIRE_NETNS=1` makes them fail instead.

### Simulated Wi-Fi tests (`test/hwsim/`)

WPA-Enterprise logins and the guest network with simulated radios
(`mac80211_hwsim`), real `hostapd` and `wpa_supplicant`.

```sh
sudo modprobe mac80211_hwsim radios=2
sudo -E go test -count=1 -v ./test/hwsim/
```

GitHub's kernel has no `mac80211_hwsim`, so CI runs them in a small
virtual machine (see the `hwsim` job in `.github/workflows/ci.yml`).

## Front end

The web UI's tests are in `web/test/`, in three groups (Vitest projects):

| Folder | What | How it runs |
| --- | --- | --- |
| `logic/` | pure functions: form checks, wording, formatting | plain Node |
| `requests/` | every API call: the method, path and body it sends | a fake `fetch` |
| `components/` | screens, with clicks and typing like a person | a simulated browser (jsdom) |

```sh
cd web
npm test                         # all of them
npx vitest run --project logic   # one group
npx vitest run test/components/vpn   # one folder
npm run coverage                 # how much of src/ the tests run
```

### Logic tests (`web/test/logic/`)

One file per module of pure functions. Add a case to the file of the
module you changed.

### Request tests (`web/test/requests/`)

One table per area. Each line says "this call sends this request", and the
helper also checks that [docs/api.md](../api.md) lists the route. To test
a new API function, add one line:

```ts
{ name: 'saves the allow list', call: () => api.putAccessSettings(settings),
  method: 'PUT', path: '/settings/access', body: settings },
```

### Component tests (`web/test/components/`)

One folder per page (`vpn/`, `hotspot/`, `system/`, …), one file per part
of the page. They render a screen with `renderScreen` and a fake device
from `fakeDevice` (the demo's sample data, changed for the test). Every
action is a spy, so a test checks what a button sent:

```tsx
const device = fakeDevice((router) => { router.vpnSettings.profiles = []; });
const { user } = renderScreen(<VpnScreen />, device, '/vpn');
await user.click(screen.getByRole('button', { name: 'Save' }));
expect(device.saveVpnDetails).toHaveBeenCalledWith('home.conf', expect.objectContaining({ … }));
```

Helpers (`web/test/components/render.tsx` and `setup.ts`):

- `fakeDevice(change?, actions?)`: `actions` replaces some spies, for
  example to make one fail.
- `openRow(user, title)`: opens a collapsed row and returns it, to look
  inside with `within(…)`.
- `section(title)`: the group of rows under a heading.
- `setViewport('desktop')`: before rendering, for the sidebar layout
  (phone is the default).
- `rerender(ui)` (from `renderScreen`): draws the screen again after the
  test changed the device.
- `finger(target, kind, clientY, time)`: a timed pointer event, for swipes
  and pulls whose speed matters.

`data/` tests the real data layer (`RealDeviceProvider`) against a fake
rbweb: `actions.test.tsx` is a table of every action, the requests it sends
and the state it leaves; `waiting.test.tsx` uses a fake clock for what takes
minutes (a restart, a factory reset, the regular reloads).

Find elements by their role and visible name, as a person would
(`getByRole('button', { name: 'Save' })`), not by class names.

## End to end

`test/e2e/walkthrough.cjs` drives the whole product in headless Chromium:
setup, every page, VPN profiles, device controls, the speed test, logging
out. It runs against a simulated device (`rbd -dev`), so nothing on the
computer changes.

```sh
cd test/e2e && npm ci && npx playwright install chromium   # once
make e2e                                                    # phone-sized screen
E2E_VIEWPORT=desktop make e2e                               # desktop-sized screen
```

`SCREENSHOT_DIR=<folder>` keeps a screenshot of each step. CI runs both
screen sizes. A new step goes where its page is in the walkthrough:

```js
await step("the guest network can be turned on", async () => {
  await open("Guests");
  // …
});
```

## Hardware

[full.md](full.md) is the test plan for a real Raspberry Pi, with each
feature's steps and what to expect. Run it before a release on at least one
board; add a section when a feature needs radios, USB adapters or real
networks to check.
