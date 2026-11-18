#!/usr/bin/env bash
# Builds exfat-fuse with RasBeacon's patch for this computer and installs
# it in place of /usr/sbin/mount.exfat-fuse, for test/netns's USB drive
# tests (CI's netns job runs it). It replaces the system's exfat-fuse: run
# it on a test machine only.
#
# The source is upstream's v1.4.0, the version Debian's package is made
# from (its files the patch changes are the same), pinned by commit. The
# image is built from Debian's own source instead (build.sh).
#
# Needs git, a C compiler, autoconf, automake, pkg-config and libfuse3's
# headers (libfuse3-dev), and sudo.
set -euo pipefail

upstream=https://github.com/relan/exfat.git
tag=v1.4.0
commit=0372fdeab2b878736fa20c9c510a0766a7996cdc
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

git clone -q --depth 1 --branch "$tag" "$upstream" "$work/exfat"
if [ "$(git -C "$work/exfat" rev-parse HEAD)" != "$commit" ]; then
  echo "error: $upstream's $tag is no longer commit $commit" >&2
  exit 1
fi
patch -d "$work/exfat" -p1 --forward < "$here/grow-without-writing.patch"
(
  cd "$work/exfat"
  autoreconf --install >/dev/null
  ./configure --quiet
  make --quiet -C libexfat
  make --quiet -C fuse
)
sudo install -m 0755 "$work/exfat/fuse/mount.exfat-fuse" /usr/sbin/mount.exfat-fuse
echo "Installed RasBeacon's exfat-fuse as /usr/sbin/mount.exfat-fuse"
