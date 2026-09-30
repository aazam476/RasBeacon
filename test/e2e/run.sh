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
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$work"' EXIT

echo "Building the web UI and both programs..."
make -C "$repo_root" build >/dev/null

echo "Starting a simulated device in $work ..."
"$repo_root/build/rbd" -dev "$work/device" >"$work/rbd.log" 2>&1 &
sleep 1
"$repo_root/build/rbweb" -listen 127.0.0.1:8080 -rbd-socket "$work/device/run/rbd/api.sock" >"$work/rbweb.log" 2>&1 &
sleep 1

cd "$repo_root/test/e2e"
[ -d node_modules ] || npm ci --no-audit --no-fund
if ! npm test; then
  echo "--- rbd log ---"; tail -n 50 "$work/rbd.log"
  echo "--- rbweb log ---"; tail -n 50 "$work/rbweb.log"
  exit 1
fi
