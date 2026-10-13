# Image definition

This folder describes the RasBeacon SD card image for
[rpi-image-gen](https://github.com/raspberrypi/rpi-image-gen).

```
config/rasbeacon.yaml          the image: one for the Pi 4/400 and Pi 5/500
                               (board, partition layout, users, locale)
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

See CONTRIBUTING.md. In short: `image/build.sh` (optionally with a version).

## One image for the Pi 4 and Pi 5

Both boards start the same standard 64-bit kernel (`kernel8.img`, 4 KB
memory pages): `config.txt` tells the Pi 5 to use it (`[pi5]
kernel=kernel8.img`) instead of its own 16 KB-page kernel, because the
read-only system is formatted for 4 KB pages. The boot partitions carry the
firmware and device trees for both boards. On the Pi 5 this costs a few per
cent of speed in memory-heavy work, which a router doesn't notice.
