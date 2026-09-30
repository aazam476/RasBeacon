#!/usr/bin/env bash
# Builds a RasBeacon SD card image with rpi-image-gen.
#
# Usage (from the repository root):
#   image/build.sh pi4|pi5 [version]
#
# Needs: a Debian/Ubuntu machine (arm64, or amd64 with qemu-user-static),
# sudo, and a checkout of rpi-image-gen in $RPI_IMAGE_GEN (default:
# ../rpi-image-gen) with its dependencies installed (install_deps.sh).
# CI runs this in .github/workflows/release.yml.
#
# Output: build/image/rasbeacon-<board>-<version>.img.xz and its SHA-256.
set -euo pipefail

board="${1:?usage: image/build.sh pi4|pi5 [version]}"
version="${2:-dev}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
rig="${RPI_IMAGE_GEN:-$repo_root/../rpi-image-gen}"
stage="$repo_root/build/image-src"
out="$repo_root/build/image"

# 1. Build the web UI and the two programs for 64-bit ARM.
make -C "$repo_root" pi VERSION="$version"

# 2. Copy image/ to a staging folder and put the programs into the layer's
#    overlay, so the repository itself never contains built binaries.
rm -rf "$stage" && mkdir -p "$stage" "$out"
cp -a "$repo_root/image/." "$stage/"
install -m 0755 "$repo_root/build/arm64/rbd" "$repo_root/build/arm64/rbweb" \
  "$stage/layer/rasbeacon.d/customize.overlay/usr/bin/"
rm -f "$stage/layer/rasbeacon.d/customize.overlay/usr/bin/.gitkeep"

# 3. Build the image.
"$rig/rpi-image-gen" build -S "$stage" -B "$repo_root/build/rig-$board" \
  -c "$stage/config/rasbeacon-$board.yaml" -- \
  IGconf_artefact_version="$version"

# 4. Compress it for Raspberry Pi Imager / balenaEtcher and record its hash.
built="$(find "$repo_root/build/rig-$board" -name "rasbeacon-$board.img" | head -n 1)"
if [ -z "$built" ]; then
  echo "Could not find the built image under build/rig-$board" >&2
  exit 1
fi
final="$out/rasbeacon-$board-$version.img.xz"
xz -T0 -9 -c "$built" > "$final"
(cd "$out" && sha256sum "$(basename "$final")" > "$(basename "$final").sha256")
echo "Built $final"
