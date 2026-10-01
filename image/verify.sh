#!/usr/bin/env bash
# Checks a freshly built image's root filesystem for things that are easy to
# get wrong and would only show up on real hardware. Run by build.sh after
# rpi-image-gen finishes; any failed check stops the build.
#
# Usage: image/verify.sh <path to the built root filesystem>
set -uo pipefail

root="${1:?usage: image/verify.sh <root filesystem directory>}"
failures=0

# check "description" command...: runs the command, reports OK or FAIL.
check() {
  local description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "OK    $description"
  else
    echo "FAIL  $description"
    failures=$((failures + 1))
  fi
}

# RasBeacon's own programs.
check "rbd is installed and executable" test -x "$root/usr/bin/rbd"
check "rbweb is installed and executable" test -x "$root/usr/bin/rbweb"

# The network programs rbd drives.
for tool in usr/sbin/hostapd usr/sbin/dnsmasq usr/sbin/nft usr/sbin/iw usr/sbin/rfkill \
  usr/sbin/wpa_supplicant usr/sbin/wpa_cli usr/bin/networkctl usr/lib/systemd/systemd-resolved \
  usr/bin/wg usr/bin/timedatectl usr/bin/zstd usr/bin/rpi-slot-tryboot; do
  check "$tool is installed" test -x "$root/$tool"
done

# Firmware for the supported USB Wi-Fi adapters (drivers alone are not enough).
check "Realtek RTL8812BU firmware (TP-Link Archer T3U)" test -f "$root/usr/lib/firmware/rtw88/rtw8822b_fw.bin"
check "Realtek RTL8812AU firmware (TP-Link Archer T4U v1/v2)" test -f "$root/usr/lib/firmware/rtw88/rtw8812a_fw.bin"
check "MediaTek MT7612U firmware" test -f "$root/usr/lib/firmware/mediatek/mt7662u.bin"
check "Built-in Wi-Fi firmware (Pi 4/5)" sh -c "ls '$root'/usr/lib/firmware/brcm/brcmfmac43455-sdio*.bin"

# Services: ours enabled, SSH and Debian's hostapd not started at boot.
check "lockdown firewall runs at boot" test -L "$root/etc/systemd/system/sysinit.target.wants/rasbeacon-firewall-early.service"
check "rbd runs at boot" test -L "$root/etc/systemd/system/multi-user.target.wants/rbd.service"
check "rbweb runs at boot" test -L "$root/etc/systemd/system/multi-user.target.wants/rbweb.service"
check "opt-in debug log is available" test -L "$root/etc/systemd/system/sysinit.target.wants/rasbeacon-debug-log.service"
check "systemd-networkd runs at boot" sh -c "ls '$root'/etc/systemd/system/*.wants/systemd-networkd.service"
check "systemd-resolved runs at boot" sh -c "ls '$root'/etc/systemd/system/*.wants/systemd-resolved.service"
check "Wi-Fi uplink service is installed" test -f "$root/usr/lib/systemd/system/rasbeacon-wpa@.service"
check "Debian's wpa_supplicant service is masked" sh -c "[ \"\$(readlink '$root/etc/systemd/system/wpa_supplicant.service')\" = /dev/null ]"
check "WireGuard is in the kernel (module or built in)" sh -c "find '$root'/usr/lib/modules -name 'wireguard.ko*' | grep -q . || grep -qs wireguard '$root'/usr/lib/modules/*/modules.builtin"
check "wg-quick is not started at boot" sh -c "! ls '$root'/etc/systemd/system/*.wants/wg-quick@*"
check "forwarding is off at boot" grep -q "^net.ipv4.ip_forward = 0" "$root/usr/lib/sysctl.d/90-rasbeacon.conf"
check "SSH is not started at boot" sh -c "! ls '$root'/etc/systemd/system/*.wants/ssh.service"
check "Debian's hostapd service is masked" sh -c "[ \"\$(readlink '$root/etc/systemd/system/hostapd.service')\" = /dev/null ]"
check "iwd is not installed" test ! -e "$root/usr/libexec/iwd"

# SSH: our settings file must be the first one OpenSSH reads.
first_dropin="$(ls "$root/etc/ssh/sshd_config.d/" | sort | head -n 1)"
check "RasBeacon's SSH settings are read first (found: $first_dropin)" test "$first_dropin" = "00-rasbeacon.conf"

# No password logins: root's and the user's passwords are locked.
check "root has no usable password" sh -c "grep -qE '^root:[*!]' '$root/etc/shadow'"
check "user rasbeacon has no usable password" sh -c "grep -qE '^rasbeacon:[*!]' '$root/etc/shadow'"

# No leftover secrets or per-device material baked into the image.
check "no RasBeacon settings baked into the image" test ! -e "$root/persistent/rasbeacon"
check "no SSH host keys baked into the image" sh -c "! ls '$root'/etc/ssh/ssh_host_*_key"

# The optional reset button's overlay is in the boot configuration.
check "reset button overlay in config.txt" grep -q "dtoverlay=gpio-key,gpio=17" "$root/boot/firmware/config.txt"

# Updates: the image knows its board; report whether it can check updates.
check "the image knows its board" test -s "$root/usr/lib/rasbeacon/board"
keys="$(ls "$root"/usr/lib/rasbeacon/update-keys/*.pub 2>/dev/null | wc -l)"
echo "INFO  update signing keys in the image: $keys (0 means update files can't be installed)"

# Licenses: every installed package ships its copyright file, including
# the Wi-Fi firmware.
check "firmware licenses are included" sh -c "ls '$root'/usr/share/doc/firmware-realtek/copyright '$root'/usr/share/doc/firmware-mediatek/copyright"

if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed."
  exit 1
fi
echo "All image checks passed."
