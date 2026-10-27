#!/bin/sh
# Builds the virtual machine the full-system test runs in (see README.md
# next to this file): a Debian trixie system with the same packages,
# programs and systemd services as the RasBeacon image, plus the test.
#
#   sudo test/system/build-vm.sh OUT_DIR
#
# It writes OUT_DIR/disk.img (the root file system), OUT_DIR/vmlinuz and
# OUT_DIR/initrd.img (the kernel to start it with). Run from the repository's
# top folder; it needs mmdebstrap, Go and e2fsprogs, and root (to make the
# files belong to root inside the machine).
set -eu

out="${1:?usage: build-vm.sh OUT_DIR}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
root="$out/rootfs"
overlay=image/layer/rasbeacon.d/customize.overlay

# 1. RasBeacon's programs, built for this computer (amd64), and the test.
mkdir -p "$out/bin"
CGO_ENABLED=0 go build -o "$out/bin/" ./cmd/rbd ./cmd/rbweb ./cmd/rbdoh
CGO_ENABLED=0 go test -c -o "$out/bin/system.test" ./test/system/

# 2. The system: the image's packages (from image/layer/rasbeacon.yaml,
#    without the Raspberry Pi firmware, Podman and the SD card tools), a
#    kernel with the simulated Wi-Fi driver, and busybox for the test
#    phones' DHCP client.
packages="systemd-sysv,linux-image-amd64,initramfs-tools,udev,kmod,iproute2,iw,rfkill,
hostapd,dnsmasq-base,dnsmasq-utils,wpasupplicant,ca-certificates,wireguard-tools,
systemd-resolved,nftables,netbase,wireless-regdb,zstd,chromium-headless-shell,fonts-dejavu-core,
busybox"
# Debian's signing keys check the packages (on Ubuntu they come from the
# debian-archive-keyring package).
keyring=""
if [ -f /usr/share/keyrings/debian-archive-keyring.gpg ]; then
	keyring="--keyring=/usr/share/keyrings/debian-archive-keyring.gpg"
fi
rm -rf "$root"
mmdebstrap --variant=apt $keyring --include="$(echo "$packages" | tr -d '\n ')" \
	--customize-hook='chroot "$1" useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin rbweb' \
	--customize-hook='chroot "$1" useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin rbdoh' \
	--customize-hook='chroot "$1" useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin rbbrowser' \
	trixie "$root" http://deb.debian.org/debian

# 3. RasBeacon's files, as the image's overlay has them.
cp -a "$overlay/." "$root/"
install -m 0755 "$out/bin/rbd" "$out/bin/rbweb" "$out/bin/rbdoh" "$root/usr/bin/"
# The image keeps settings on its own partitions; here they are folders.
mkdir -p "$root/persistent" "$root/bootfs"

# 4. The test: its program, the world it builds before rbd starts
#    (radios, hotel, VPN server, phones), and the run itself.
install -m 0755 "$out/bin/system.test" "$root/usr/local/bin/rasbeacon-system.test"
cp test/system/units/*.service "$root/etc/systemd/system/"

# 5. Which services start. As in the image: the early firewall, rbd and
#    rbweb, networkd and resolved; Debian's own hostapd and wpa_supplicant
#    services stay off (rbd starts its own). The test's two services too.
chroot "$root" systemctl enable rasbeacon-firewall-early.service rbd.service rbweb.service \
	systemd-networkd.service systemd-resolved.service \
	rasbeacon-test-world.service rasbeacon-system-test.service
chroot "$root" systemctl mask hostapd.service wpa_supplicant.service serial-getty@ttyS0.service getty@tty1.service
# The machine boots straight into the test: no login, no password.
echo rasbeacon > "$root/etc/hostname"

# 6. The kernel to start it with, and the disk.
cp "$root"/boot/vmlinuz-* "$out/vmlinuz"
cp "$root"/boot/initrd.img-* "$out/initrd.img"
rm -f "$out/disk.img"
mkfs.ext4 -q -L rasbeacon -d "$root" "$out/disk.img" 6G
rm -rf "$root"
echo "built $out/disk.img"
