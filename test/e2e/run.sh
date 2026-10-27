#!/usr/bin/env bash
# Builds RasBeacon, starts a simulated device, and runs the browser
# walkthrough against it. Usage (from the repository root):
#
#   test/e2e/run.sh
#
# Needs Go, Node.js, and Playwright's Chromium (`npx playwright install
# chromium` inside test/e2e, or set CHROMIUM_PATH).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
# Stop rbd and rbweb, and wait until they have exited before removing the
# folder: rbd saves the clock and data use as it stops.
trap 'kill $(jobs -p) 2>/dev/null || true; wait 2>/dev/null || true; rm -rf "$work"' EXIT

echo "Building the web UI and both programs..."
make -C "$repo_root" build >/dev/null

# The zero-leak login and the speed test need a headless Chromium for the simulated device's
# own browser: Playwright's, Debian's, or RASBEACON_BROWSER.
login_browser="${RASBEACON_BROWSER:-}"
for candidate in /usr/bin/chromium-headless-shell \
  "$HOME"/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell \
  "$HOME"/.cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell \
  /opt/pw-browsers/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell \
  /opt/pw-browsers/chromium_headless_shell-*/chrome-linux/headless_shell; do
  [ -n "$login_browser" ] && break
  [ -x "$candidate" ] && login_browser="$candidate"
done
if [ -n "$login_browser" ]; then
  echo "Login browser for the simulated device: $login_browser"
  export RASBEACON_E2E_HOTEL=http://127.0.0.1:8099
else
  echo "No headless Chromium found: the zero-leak login and speed test steps are skipped."
  if [ "${RASBEACON_REQUIRE_BROWSER:-}" = 1 ]; then exit 1; fi
fi

echo "Starting a simulated device in $work ..."
"$repo_root/build/rbd" -dev "$work/device" -dev-hotel 127.0.0.1:8099 -dev-browser "$login_browser" >"$work/rbd.log" 2>&1 &
# rbd opens its API only once the (simulated) hotspot has started, a few
# seconds in: wait for it (up to 30 s), so the first page finds it.
for _ in $(seq 60); do
  [ -S "$work/device/run/rbd/api.sock" ] && break
  sleep 0.5
done
if [ ! -S "$work/device/run/rbd/api.sock" ]; then
  echo "rbd didn't start:"; tail -n 50 "$work/rbd.log"; exit 1
fi
"$repo_root/build/rbweb" -listen 127.0.0.1:8080 -rbd-socket "$work/device/run/rbd/api.sock" >"$work/rbweb.log" 2>&1 &
# And for rbweb to listen (up to 10 s): bash opens a TCP connection itself.
for _ in $(seq 20); do
  (exec 3<>/dev/tcp/127.0.0.1/8080) 2>/dev/null && break
  sleep 0.5
done
if ! (exec 3<>/dev/tcp/127.0.0.1/8080) 2>/dev/null; then
  echo "rbweb didn't start:"; tail -n 50 "$work/rbweb.log"; exit 1
fi

cd "$repo_root/test/e2e"
[ -d node_modules ] || npm ci --no-audit --no-fund
if ! npm test; then
  echo "--- rbd log ---"; tail -n 50 "$work/rbd.log"
  echo "--- rbweb log ---"; tail -n 50 "$work/rbweb.log"
  exit 1
fi
