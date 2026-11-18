#!/usr/bin/env bash
# Lists the packages in a released image that have a newer version in the
# archives the image is built from: Debian trixie with its updates and
# security fixes, and Raspberry Pi's archive (the kernel and firmware), as
# rpi-image-gen's templates/debian/apt/{trixie,rpi-trixie}.sources name
# them. Building the release again would bring those versions in.
#
# The daily workflow .github/workflows/debian-updates.yml runs it with the
# newest release's package lists, and builds a patch release when it
# finds something. The indexes are read over HTTPS without checking
# Debian's signatures: all they can do is start a build, whose apt checks
# every package it installs.
#
# Usage: image/check-updates.sh <name>-<version>.packages.txt...
#
# Prints one line per package, its newest version first found in the
# archive named:
#   openssl 3.5.1-1 -> 3.5.1-1+deb13u1 (trixie-security)
# Exits 0 when nothing is newer, 3 when something is, 1 on a problem.
set -euo pipefail

if [ $# -eq 0 ]; then
  echo "usage: image/check-updates.sh <name>-<version>.packages.txt..." >&2
  exit 1
fi
for list in "$@"; do
  if [ ! -s "$list" ]; then
    echo "$list is missing or empty" >&2
    exit 1
  fi
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# label|base URL|suite|components
archives=(
  "trixie|https://deb.debian.org/debian|trixie|main contrib non-free non-free-firmware"
  "trixie-updates|https://deb.debian.org/debian|trixie-updates|main contrib non-free non-free-firmware"
  "trixie-security|https://deb.debian.org/debian-security|trixie-security|main contrib non-free non-free-firmware"
  "raspberrypi|https://archive.raspberrypi.com/debian|trixie|main"
)

# index URL: prints the index of packages at URL (without .xz or .gz), in
# whichever compression the archive offers (Debian's .xz, Raspberry Pi's
# .gz). It fails if there is none.
index() {
  if curl -fsSL --retry 3 --max-time 300 "$1.xz" -o "$work/index.xz" 2>/dev/null; then
    xz -dc "$work/index.xz"
  elif curl -fsSL --retry 3 --max-time 300 "$1.gz" -o "$work/index.gz" 2>/dev/null; then
    gzip -dc "$work/index.gz"
  else
    curl -fsSL --retry 3 --max-time 300 "$1" 2>/dev/null
  fi
}

# Every package version the archives have, one "package version label" a
# line. Debian lists the packages for any architecture ("all") in
# binary-all only; Raspberry Pi's archive has no binary-all.
: >"$work/available"
for entry in "${archives[@]}"; do
  IFS='|' read -r label base suite components <<<"$entry"
  for component in $components; do
    for arch in arm64 all; do
      url="$base/dists/$suite/$component/binary-$arch/Packages"
      if ! index "$url" >"$work/index"; then
        # Each archive has its main packages for arm64; the rest may not
        # exist (an empty component, or no "all").
        if [ "$component" = main ] && [ "$arch" = arm64 ]; then
          echo "could not read $url" >&2
          exit 1
        fi
        continue
      fi
      awk -v label="$label" '/^Package: /{p=$2} /^Version: /{print p, $2, label}' "$work/index" >>"$work/available"
    done
  done
done

# The image's packages: "package version" (the lists also name the source
# package; see build.sh).
cat "$@" | awk 'NF >= 2 {print $1, $2}' | sort -u >"$work/image"

# The archives' versions that differ from the image's, for the packages
# the image has: "package image-version archive-version label".
awk 'NR == FNR {have[$1] = $2; next} ($1 in have) && $2 != have[$1] {print $1, have[$1], $2, $3}' \
  "$work/image" "$work/available" | sort -u >"$work/candidates"

# Of those, the newer ones (dpkg's own comparison: epochs, "~" and "+"),
# the newest for each package.
declare -A newest newest_label image_version
while read -r package installed candidate label; do
  dpkg --compare-versions "$candidate" gt "$installed" || continue
  if [ -z "${newest[$package]:-}" ] || dpkg --compare-versions "$candidate" gt "${newest[$package]}"; then
    newest[$package]="$candidate"
    newest_label[$package]="$label"
    image_version[$package]="$installed"
  fi
done <"$work/candidates"

if [ "${#newest[@]}" -eq 0 ]; then
  exit 0
fi
for package in $(printf '%s\n' "${!newest[@]}" | sort); do
  echo "$package ${image_version[$package]} -> ${newest[$package]} (${newest_label[$package]})"
done
exit 3
