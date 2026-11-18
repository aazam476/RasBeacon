# RasBeacon's exfat-fuse

RasBeacon reads exFAT USB drives with `exfat-fuse`, a program, confined as
a user of its own ([internal/fusedrive](../../internal/fusedrive/fusedrive.go)),
rather than the kernel. The image has Debian's `exfat-fuse` with one patch,
[grow-without-writing.patch](grow-without-writing.patch).

## What the patch changes

A share on a USB drive holds the room its quota promises with files grown
to that size (its reservation, in the drive's `RasBeacon reserved space`
folder). Debian's `exfat-fuse` writes zeros over the new part of a file
whenever it grows, so holding a 500 GB quota meant writing 500 GB to the
drive. exFAT doesn't need that: what lies past a file's *valid data
length* reads as zeros. Windows and Linux's own exFAT driver grow a file by
allocating its clusters only, and write zeros when data is later written
past that length, over the part in between.

With the patch, `exfat-fuse` does the same. Reading already returned zeros
past the valid data length, files made this way are valid exFAT (Windows
makes them too), and `fsck.exfat` finds the drive clean. A marker string
in the program lets `image/verify.sh` tell this build from Debian's.
`test/netns` (`TestDrives_ExFATFilesGrowWithoutWritingZeros`) checks it all
on a real exFAT drive.

## How it is built

- **The image:** `image/build.sh` runs [build.sh](build.sh), which makes a
  fresh Debian trixie with the archives the image comes from, fetches
  Debian's current `exfat-fuse` source there (checked by apt against
  Debian's signatures), applies the patch and builds the package
  ([in-chroot.sh](in-chroot.sh)). The image installs it in place of
  Debian's (`layer/rasbeacon.yaml`).
- **CI's drive tests:** [build-for-tests.sh](build-for-tests.sh) builds
  upstream's v1.4.0, which Debian's package is made from, with the patch,
  for the test machine.

## Debian's fixes

The package keeps Debian's version, so the image's package list
(`<name>-<version>.packages.txt`) shows Debian's `exfat-fuse` version, and
the daily check for Debian's fixes (`image/check-updates.sh`) watches it
like any other package. When Debian fixes `exfat-fuse`, the check starts a
release, and that build applies the patch to the fixed source.

If a fix changes the lines the patch changes, the patch doesn't apply, and
the image build stops with `patch`'s error: nothing ships without the
driver. Then bring the patch up to date with the new source (apply it by
hand, `diff -u` the four files again, keep the description at the top) and
check it with `image/exfat-fuse/build-for-tests.sh` (update its version and
commit) and `sudo -E go test ./test/netns/ -run TestDrives_`.

The patch could go to `exfat-fuse`'s authors
([relan/exfat](https://github.com/relan/exfat)); once Debian's package has
the change, this folder can go.
