# Image definition

This folder describes the RasBeacon SD card images for
[rpi-image-gen](https://github.com/raspberrypi/rpi-image-gen): one for the
Pi 4 and Pi 5, one for the Pi Zero 2 W, 3B and 3B+. Both have the same programs and
packages; only the board layer and the partition layout differ.

```
config/rasbeacon-common.yaml   shared settings (users, layers, SSH, locale)
config/rasbeacon.yaml          the Pi 4/400 and Pi 5/500 image (A/B, GPT)
config/rasbeacon-pi3-zero2w.yaml   the Pi Zero 2 W, 3B and 3B+ image (one system, MBR)
layer/rasbeacon-base.yaml      minimal Debian trixie system (without iwd)
layer/rasbeacon.yaml           RasBeacon's packages and setup steps
layer/rasbeacon.d/customize.overlay/   files copied into the image as-is:
    usr/lib/systemd/system/    services: lockdown firewall, rbd, rbweb, hostapd, dnsmasq
    usr/lib/rasbeacon/lockdown.nft   firewall loaded before networking starts
    usr/lib/sysctl.d/          no packet forwarding by default
    etc/ssh/sshd_config.d/     SSH: keys only, keys stored on the data partition
    etc/systemd/journald.conf.d/   logs kept in memory only
layer/rasbeacon-single.yaml    that image only: /persistent as a folder, /bootfs
                               as the boot partition (bootfs.mount)
build.sh                       builds one image: rpi64 or rpi3 (used by CI)
```

## Disk layout, Pi 4/5 image ("image-rota", from rpi-image-gen)

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

## The Pi Zero 2 W, 3B and 3B+ image

These boards share a chip family, boot ROM and kernel. Their boot ROM only reads an MBR partition table and starts
`bootcode.bin` from the first partition; it can't follow the Pi 4/5's A/B
selection (`autoboot.txt`, GPT). So its image uses rpi-image-gen's
"image-rpios" layout:

| Partition | Mounted at | Contents |
|---|---|---|
| `BOOT` (FAT) | `/boot/firmware`, and `/bootfs` | Firmware, kernel, `config.txt`; **the drive you see on a computer**, where `setup.txt`, `factory-reset` and `debug` go |
| `ROOT` (ext4, writable) | `/` | The operating system, and `/persistent` (settings) as a folder |

There is one copy of the system, so the System page says new versions are
flashed (rbd reads the board, `rpi3`, from `/usr/lib/rasbeacon/board`).
The root partition grows to fill the card on the first start. Everything
else is the same as on the Pi 4/5: the same kernel (`kernel8.img`), the same
programs, and the same settings in `/persistent/rasbeacon`.
