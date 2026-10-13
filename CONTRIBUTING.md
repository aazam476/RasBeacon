# Contributing to RasBeacon

Thanks for helping! This project is written to be readable by a junior
engineer or a second- or third-year computer science student. If something
in the code is hard to follow, that is a bug worth reporting.

## Repository layout

```
cmd/rbd/            rbd: privileged background program (network agent)
cmd/rbweb/          rbweb: web server for the settings pages
cmd/rbsign/         release tool: makes the update signing key, signs update files
internal/agent/     rbd's logic: boot sequence, modes, settings, API
internal/webserver/ rbweb's logic: sessions, login, forwarding to rbd
internal/render/    config generators; templates/ holds the commented config templates
internal/config/    settings types, validation, crash-safe file writing
internal/...        small focused packages (radio detection, setup.txt, ...)
internal/devsim/    simulated device for development without a Pi
web/                the web UI (React + TypeScript)
image/              Raspberry Pi image definition (rpi-image-gen layers, systemd units)
test/netns/         tests with real firewall/DHCP/DNS/WireGuard in network namespaces
test/hwsim/         WPA-Enterprise logins with simulated Wi-Fi radios
test/e2e/           browser walkthrough of the web UI
docs/               design, architecture, API, UI contract, glossary, test plans
```

## Tools you need

- **Go 1.24 or newer.**
- **Node.js 22 or newer.**
- **Linux**, for the network namespace tests and image builds. Everything
  else also works on macOS.

## Everyday commands

```sh
make test     # all unit tests (Go and web UI)
make lint     # formatting and static checks, the same as CI
make build    # build the web UI and both programs into build/
make dev      # simulated device at http://127.0.0.1:8080
make e2e      # browser walkthrough against a simulated device
make pi       # build both programs for the Raspberry Pi (64-bit ARM)
```

- **Network namespace tests** (Linux, needs root, plus `nftables`,
  `dnsmasq`, `busybox` and `wireguard-tools`):
  `sudo -E go test ./test/netns/ -v`. The VPN tests need WireGuard in the
  kernel; where it is missing (some containers), they use `wireguard-go`
  if it is on the `PATH` (`go install golang.zx2c4.com/wireguard@latest`,
  then name the binary `wireguard-go`).
- **Simulated Wi-Fi tests** (Linux, needs root, `hostapd`, `wpasupplicant`
  and the `mac80211_hwsim` module):
  `sudo modprobe mac80211_hwsim radios=2 && sudo -E go test ./test/hwsim/ -v`.
  CI runs them in a small virtual machine because GitHub's kernel lacks
  the module (see `.github/workflows/ci.yml`).
- **Browser test:** it needs Playwright's Chromium once:
  `cd test/e2e && npm ci && npx playwright install chromium`.

## Running a simulated device

`rbd -dev <dir>` runs RasBeacon as if on a Raspberry Pi with a built-in
radio and a USB adapter, but keeps all files under `<dir>` and only *logs*
the commands it would run. Nothing on your computer changes. `make dev`
starts it with `rbweb` on `http://127.0.0.1:8080`.

To work on the web UI with instant reload, start the simulated device in one
terminal and `cd web && npm run dev` in another. To get back to first-time
setup, stop everything and delete the `-dev` directory. Without any device,
`cd web && npm run demo` runs the UI on a demo router with every state
listed on `/#/demo`.

## Building an SD card image

Images are normally built by GitHub Actions
(`.github/workflows/release.yml`). To build one yourself on Debian or Ubuntu
(arm64 is fastest; amd64 works with QEMU emulation):

```sh
git clone https://github.com/raspberrypi/rpi-image-gen.git ../rpi-image-gen
sudo ../rpi-image-gen/install_deps.sh
image/build.sh            # one image for the Pi 4 and Pi 5
```

## Code style

These rules are also in [docs/design.md](docs/design.md) (section 9a).

- **Every file starts with a comment:** what it is for and how it fits in.
- **Every function, type and constant has a comment** that says what it
  does and **why** it exists.
- **Plain code over clever code:**
  - descriptive names
  - small functions
  - no reflection or code generation
  - no goroutine tricks (rbd changes state under one lock, one change at a
    time)
- **Configs are templates** (`internal/render/templates/*.tmpl`) that look
  like the real file, with a comment on each rule. When you change one, run
  `go test ./internal/render -update` and review the diff of the
  `testdata/*.golden` files.
- **Error messages are full sentences** that say what went wrong and what
  to do. Validation messages are shown to users as-is.
- **Tests describe behaviour** in their names, e.g.
  `TestFirstBoot_BrokenSetupTxt_KeepsNetworkOffInsteadOfOpen`.
- **Web UI:** screens only read `useDevice()` and call its actions; talking
  to the router is the provider's job (`web/src/data/real/`), and its pure
  logic is in `logic.ts` with unit tests. See
  [docs/ui-contract.md](docs/ui-contract.md) and [web/README.md](web/README.md).

## Commit messages

Start the subject with a type prefix:

| Prefix | Use for |
|---|---|
| `feat:` | New feature |
| `fix:` | Bug fix |
| `chore:` | Tooling, config, dependencies |
| `refactor:` | Code change that isn't a fix or feature |
| `docs:` | Documentation only |
| `style:` | Formatting, no logic change |
| `test:` | Adding or fixing tests |

## Adding hardware to the compatibility table

If you test a board, USB Wi-Fi adapter or power bank, add a row to
[docs/compatibility.md](docs/compatibility.md) with what you measured, and
say how (for example "iPhone + 9 other devices associated for 30 min").
Measured numbers are much more useful than "works for me".
