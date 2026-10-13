# Making a release

Releases are built by GitHub Actions (`.github/workflows/release.yml`).
Pushing a tag like `v1.2.0` builds the images and creates a **draft**
release; check it, then publish it.

There is one image for the Pi 4 and Pi 5 (board name `rpi64`), and one for
the Pi Zero 2 W, 3B and 3B+ (`rpi3`: same programs, MBR layout with one
system copy, so it has no update file). Each release contains:

| File | What it is |
|---|---|
| `rasbeacon-<version>.img.xz` | The SD card image for the Pi 4 and Pi 5 (flash it with Raspberry Pi Imager) |
| `rasbeacon-pi3-zero2w-<version>.img.xz` | The SD card image for the Pi Zero 2 W, 3B and 3B+ |
| `rasbeacon-<version>.rbupdate` | The signed update file (install it on the System page) |
| `rasbeacon-<version>.packages.txt`, `rasbeacon-pi3-zero2w-<version>.packages.txt` | Every Debian package in the image, with its source package and version (see [source-code.md](source-code.md)) |
| `SHA256SUMS` | Checksums of all of the above |

## One-time setup: the update signing key

RasBeacon only installs update files signed with a key it knows. The
public half is built into the image; the private half signs releases.

1. On your own computer (not a shared one), in the repository:
   ```sh
   go run ./cmd/rbsign keygen
   ```
   It prints a **private key** and a **public key**.
2. In the GitHub repository: *Settings → Secrets and variables → Actions →
   New repository secret*, name `RASBEACON_UPDATE_SIGNING_KEY`, value: the
   private key. Then clear your terminal (`clear` and close it); don't save
   the private key anywhere else unless you have a safe place (a password
   manager).
3. Save the public key as
   `image/layer/rasbeacon.d/customize.overlay/usr/lib/rasbeacon/update-keys/release.pub`
   and commit it.

Images built **before** step 3 have no key and can't install update files;
their owners update by flashing once. From then on, updates work.

**If the private key leaks:** make a new key pair, add the new public key
as a second file (`release-2.pub`), make a release signed with the *old*
key that contains both public keys, then switch the secret to the new key
and, in a later release, remove the old public key.

**Forks** don't have the secret, so their builds make images but no update
files.

## Checklist for a release

1. CI is green on the commit you'll tag.
2. Update `docs/compatibility.md` with any new hardware results.
3. Tag and push: `git tag v1.2.0 && git push origin v1.2.0`.
4. When the workflow finishes, open the draft release, check the files are
   all there (the two images, the update file, the two package lists,
   `SHA256SUMS`), write the notes, and publish.
5. Test the update file on a device running the previous release (System
   page → Install update), and check it comes back on the new version.

## How updates work on the device

See `internal/agent/update.go`. In short: the signature and board are
checked first; the new system is written into the slot that isn't
running; the SHA-256 of everything written must match the signed one;
RasBeacon then restarts into the new slot *on trial*. If it runs normally
for a minute, the new slot becomes the default; if not, the device goes
back to the previous version by itself.
