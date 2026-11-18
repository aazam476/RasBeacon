#!/usr/bin/env bash
# Builds RasBeacon's exfat-fuse for the image: Debian's exfat-fuse, from
# Debian's source, with grow-without-writing.patch (see README.md here),
# as a .deb in <out-folder>.
#
# Usage (from the repository root; needs mmdebstrap, sudo and the
# internet, and on a computer that isn't 64-bit ARM, QEMU's user
# emulation):
#   image/exfat-fuse/build.sh <out-folder> [architecture]
# The architecture is the image's, arm64, unless given (amd64 to try it on
# a PC).
#
# It builds in a fresh Debian trixie with the archives the image is built
# from, so the source is the version the image would otherwise get, and the
# .deb keeps that version: image/check-updates.sh sees a fix Debian makes
# to it as for any other package, and the release it starts builds the
# fixed source with the patch on top. Should a fix change the lines the
# patch changes, the patch doesn't apply and this stops, and with it the
# image build, until the patch is brought up to date.
set -euo pipefail

out="${1:?usage: image/exfat-fuse/build.sh <out-folder> [architecture]}"
arch="${2:-arm64}"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$out"
out="$(cd "$out" && pwd)"

if ! command -v mmdebstrap >/dev/null; then
  echo "error: mmdebstrap is needed to build exfat-fuse (apt-get install mmdebstrap debian-archive-keyring)" >&2
  exit 1
fi
# Debian's signing keys check everything apt fetches (on Ubuntu they come
# from the debian-archive-keyring package).
keyring=()
if [ -f /usr/share/keyrings/debian-archive-keyring.gpg ]; then
  keyring=(--keyring=/usr/share/keyrings/debian-archive-keyring.gpg)
fi

work="$(mktemp -d)"
trap 'sudo rm -rf "$work"' EXIT
cp "$here/grow-without-writing.patch" "$here/in-chroot.sh" "$work/"

# The archives the image is built from, with their source packages.
archives=(
  "deb http://deb.debian.org/debian trixie main"
  "deb-src http://deb.debian.org/debian trixie main"
  "deb http://deb.debian.org/debian trixie-updates main"
  "deb-src http://deb.debian.org/debian trixie-updates main"
  "deb http://deb.debian.org/debian-security trixie-security main"
  "deb-src http://deb.debian.org/debian-security trixie-security main"
)

sudo mmdebstrap --mode=root --variant=buildd --architectures="$arch" "${keyring[@]}" \
  --customize-hook="copy-in $work/grow-without-writing.patch $work/in-chroot.sh /tmp" \
  --customize-hook='chroot "$1" sh /tmp/in-chroot.sh' \
  trixie "$work/root" "${archives[@]}"

deb="$(find "$work/root/build" -maxdepth 1 -name "exfat-fuse_*_$arch.deb" | head -n 1)"
if [ -z "$deb" ]; then
  echo "error: building exfat-fuse made no exfat-fuse_*_$arch.deb" >&2
  exit 1
fi
# The driver in it must be the patched one.
if ! dpkg-deb --fsys-tarfile "$deb" | tar -xO ./usr/sbin/mount.exfat-fuse | grep -aq "RasBeacon exfat-fuse: files grow without writing zeros"; then
  echo "error: $deb has no patched mount.exfat-fuse" >&2
  exit 1
fi
cp "$deb" "$out/"
echo "Built $out/$(basename "$deb")"
