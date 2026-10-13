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
# Output, in build/image/:
#   rasbeacon-<board>-<version>.img.xz        the SD card image
#   rasbeacon-<board>-<version>.rbupdate      signed update file (only when the
#                                              RASBEACON_UPDATE_SIGNING_KEY
#                                              environment variable is set)
#   rasbeacon-<board>-<version>.packages.txt  every Debian package in the
#                                              image, with its source package
#                                              and version (for the GPL)
#   a .sha256 file for each of the above
set -euo pipefail

board="${1:?usage: image/build.sh pi4|pi5 [version]}"
version="${2:-dev}"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
rig="${RPI_IMAGE_GEN:-$repo_root/../rpi-image-gen}"
stage="$repo_root/build/image-src"
out="$repo_root/build/image"

# 1. Build the web UI and the programs for 64-bit ARM.
make -C "$repo_root" pi VERSION="$version" REPOSITORY="${GITHUB_REPOSITORY:-}"

# 2. Copy image/ to a staging folder and put the programs into the layer's
#    overlay, so the repository itself never contains built binaries.
rm -rf "$stage" && mkdir -p "$stage" "$out"
cp -a "$repo_root/image/." "$stage/"
install -m 0755 "$repo_root/build/arm64/rbd" "$repo_root/build/arm64/rbweb" "$repo_root/build/arm64/rbdoh" \
  "$stage/layer/rasbeacon.d/customize.overlay/usr/bin/"
rm -f "$stage/layer/rasbeacon.d/customize.overlay/usr/bin/.gitkeep"
# Which board this image is for: update files must match it.
echo "$board" > "$stage/layer/rasbeacon.d/customize.overlay/usr/lib/rasbeacon/board"

# 3. Build the image (rpi-image-gen needs its work directory to exist).
mkdir -p "$repo_root/build/rig-$board"
"$rig/rpi-image-gen" build -S "$stage" -B "$repo_root/build/rig-$board" \
  -c "$stage/config/rasbeacon-$board.yaml" -- \
  IGconf_artefact_version="$version"

# 4. Check the result (firmware present, SSH settings, no baked-in secrets).
#    The build tree is partly owned by root, hence sudo.
rootfs="$(sudo find "$repo_root/build/rig-$board" -maxdepth 3 -type d -path "*/chroot-*/filesystem" | head -n 1)"
sudo "$repo_root/image/verify.sh" "$rootfs"

# 4b. List every package and its source, so the matching source code can be
#     found (see docs/source-code.md).
packages="$out/rasbeacon-$board-$version.packages.txt"
sudo dpkg-query --admindir="$rootfs/var/lib/dpkg" -W \
  -f '${Package} ${Version} source=${source:Package} ${source:Version}\n' | sort > "$packages"

# 5. Compress it for Raspberry Pi Imager / balenaEtcher and record its hash.
# rpi-image-gen writes the raw image to <work dir>/image-<name>/<name>.img.
built="$repo_root/build/rig-$board/image-rasbeacon-$board/rasbeacon-$board.img"
if [ ! -f "$built" ]; then
  echo "Could not find the built image at $built" >&2
  exit 1
fi
final="$out/rasbeacon-$board-$version.img.xz"
xz -T0 -6 -c "$built" > "$final"

# 6. The signed update file, made from rpi-image-gen's update bundle (the
#    boot and system partitions). Without the signing key (for example in
#    a fork), no update file is made; the image itself is unaffected.
bundle="$(sudo find "$repo_root/build/rig-$board/image-rasbeacon-$board" -maxdepth 1 -name '*.update.tar.zst' | head -n 1)"
if [ -n "${RASBEACON_UPDATE_SIGNING_KEY:-}" ] && [ -n "$bundle" ]; then
  sudo cat "$bundle" > "$out/bundle.tmp"
  go run "$repo_root/cmd/rbsign" pack -board "$board" -version "$version" \
    -bundle "$out/bundle.tmp" -out "$out/rasbeacon-$board-$version.rbupdate"
  rm -f "$out/bundle.tmp"
else
  echo "No update file made (no signing key, or no update bundle at '$bundle')."
fi

(cd "$out" && for f in rasbeacon-$board-$version.*; do
  case "$f" in *.sha256) ;; *) sha256sum "$f" > "$f.sha256" ;; esac
done)
echo "Built $final"
