#!/usr/bin/env bash
# Signs the update bundle that image/build.sh left in a folder: makes the
# update file (.rbupdate) and its .update.json there, with their checksums,
# and removes the bundle.
#
# Usage (from the repository root):
#   image/sign-update.sh <folder> <version>
#
# The signing key is read from the RASBEACON_UPDATE_SIGNING_KEY environment
# variable (by rbsign: never from the command line, which other programs
# could see). See docs/releasing.md.
#
# It is a script of its own so the release can sign on a machine that has
# done nothing else. Whatever runs in the same job as the key can read it:
# a program finds the environment of the script that started it in /proc,
# even after the script removed the variable. Building the image runs npm,
# its packages' install scripts, apt and the image tools; this script runs
# rbsign, which needs only Go's standard library.
set -euo pipefail

dir="${1:?usage: image/sign-update.sh <folder> <version>}"
version="${2:?usage: image/sign-update.sh <folder> <version>}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"

# Only the Pi 4 and Pi 5 image (board rpi64, named rasbeacon) has two
# system copies, and so an update file.
board=rpi64
name=rasbeacon
bundle="$dir/update-bundle.tar.zst"

if [ -z "${RASBEACON_UPDATE_SIGNING_KEY:-}" ]; then
  echo "error: RASBEACON_UPDATE_SIGNING_KEY is not set, so the update file can't be signed." >&2
  exit 1
fi
# With the signing key set (a release), going on without the bundle would
# publish a release nobody can update to.
if [ ! -s "$bundle" ]; then
  echo "error: there is no update bundle at $bundle (image/build.sh keeps it there with RASBEACON_KEEP_UPDATE_BUNDLE=1), so no update file can be made." >&2
  exit 1
fi

# The oldest version that can install this update: image/update-from,
# without its comment lines (empty: any version).
min_from="$( { grep -v '^#' "$repo_root/image/update-from" || true; } | tr -d '[:space:]')"
# -trusted: the public keys this image trusts. rbsign prints which one
# belongs to the signing key, and stops if none does (devices would
# refuse the update).
go run "$repo_root/cmd/rbsign" pack -board "$board" -version "$version" -min-from "$min_from" \
  -bundle "$bundle" -out "$dir/$name-$version.rbupdate" -info "$dir/$name-$version.update.json" \
  -trusted "$repo_root/image/layer/rasbeacon.d/customize.overlay/usr/lib/rasbeacon/update-keys"
rm -f "$bundle"

(cd "$dir" && for f in "$name-$version.rbupdate" "$name-$version.update.json"; do
  sha256sum "$f" > "$f.sha256"
done)
echo "Signed $dir/$name-$version.rbupdate"
