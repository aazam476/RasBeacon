# Image definition

This folder describes the RasBeacon SD card image for
[rpi-image-gen](https://github.com/raspberrypi/rpi-image-gen).

```
config/rasbeacon-common.yaml   settings shared by all boards (layout, users, locale)
config/rasbeacon-pi4.yaml      Pi 4 / 400 (also Pi Zero 2 W and Pi 3: same kernel)
config/rasbeacon-pi5.yaml      Pi 5 / 500
layer/rasbeacon-base.yaml      minimal Debian trixie system (without iwd)
layer/rasbeacon.yaml           RasBeacon's packages and setup steps
layer/rasbeacon.d/customize.overlay/   files copied into the image as-is:
    usr/lib/systemd/system/    services: lockdown firewall, rbd, rbweb, hostapd, dnsmasq
    usr/lib/rasbeacon/lockdown.nft   firewall loaded before networking starts
    usr/lib/sysctl.d/          no packet forwarding by default
    etc/ssh/sshd_config.d/     SSH: keys only, keys stored on the data partition
    etc/systemd/journald.conf.d/   logs kept in memory only
build.sh                       builds one image (used by CI)
```

## Disk layout ("image-rota", from rpi-image-gen)

| Partition | Mounted at | Contents |
|---|---|---|
| `BOOTCONFIG` (FAT, 32 MB) | `/bootfs` | A/B boot selection. **This is the drive you see on a computer**, where `setup.txt` and `factory-reset` go. |
| `boot_a`, `boot_b` (FAT) | `/boot/firmware` (active one, read-only) | Kernel and firmware, one copy per slot |
| `system_a`, `system_b` | `/` (active one, read-only) | The operating system, one copy per slot |
| `persistent` (ext4) | `/persistent` | Settings (`/persistent/rasbeacon`), SSH host keys |

The system is read-only, so a power cut can't corrupt it. Updates will write
the other slot and switch over with automatic rollback (milestone 6).

## Building

See CONTRIBUTING.md. In short: `image/build.sh pi4` or `image/build.sh pi5`.
