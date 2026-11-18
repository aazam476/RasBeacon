#!/bin/sh
# Runs inside the Debian trixie that image/exfat-fuse/build.sh makes:
# fetches exfat-fuse's source and what building it needs, applies
# RasBeacon's patch and builds the .deb in /build.
set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get build-dep -y -q fuse-exfat
mkdir /build
cd /build
apt-get source -q fuse-exfat
cd fuse-exfat-*/
# --forward: a patch Debian already took is an error, not applied twice.
patch -p1 --forward < /tmp/grow-without-writing.patch
dpkg-buildpackage -b -uc -us
