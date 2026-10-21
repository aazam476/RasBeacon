# Making a release

Releases are built by GitHub Actions (`.github/workflows/release.yml`).
Pushing a tag like `v0.1.0` builds the images and creates a **draft**
release; check it, then publish it.

## Version numbers

RasBeacon's versions are `vMAJOR.MINOR.PATCH`:

- **PATCH** (`v0.1.1`): bug fixes only.
- **MINOR** (`v0.2.0`): new features. Before 1.0, a minor version may also
  change things plugins rely on.
- **MAJOR** (`v1.0.0`): incompatible changes (after 1.0).

Versions before 1.0 are early versions: a `0` at the front already says
so, and they are published as normal releases (`v0.1.0`), which devices are
offered as updates.

To try out a version before releasing it, give it a label:
`v0.2.0-beta.1`. Publish it as a GitHub **pre-release** (tick *Set as a
pre-release*): devices' update check reads the latest release, which never
includes pre-releases, so nobody is offered a test version by surprise.

Plugins follow along: the plugins repository's release tags start with the
RasBeacon version they're made for (`v0.1.0`, `v0.1.1`, … for RasBeacon
0.1), and a plugin that needs a newer RasBeacon says so in its manifest
(`minRasBeacon`, see [plugins.md](plugins.md)).

There is one image for the Pi 4 and Pi 5 (board name `rpi64`), and one for
the Pi Zero 2 W, 3B and 3B+ (`rpi3`: same programs, MBR layout with one
system copy, so it has no update file). Each release contains:

| File | What it is |
|---|---|
| `rasbeacon-<version>.img.xz` | The SD card image for the Pi 4 and Pi 5 (flash it with Raspberry Pi Imager) |
| `rasbeacon-pi3-zero2w-<version>.img.xz` | The SD card image for the Pi Zero 2 W, 3B and 3B+ |
| `rasbeacon-<version>.rbupdate` | The signed update file (install it on the System page) |
| `rasbeacon-<version>.update.json` | The update's version and the oldest version that can install it, for the online check |
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

Only `rbsign` ever sees the key: `image/build.sh` takes it out of the
environment before it runs anything else (npm and its packages' install
scripts included), and only the release workflow's `publish` job may write
to the repository.

**If the private key leaks:** make a new key pair, add the new public key
as a second file (`release-2.pub`), make a release signed with the *old*
key that contains both public keys, then switch the secret to the new key
and, in a later release, remove the old public key.

**Forks** don't have the secret, so their builds make images but no update
files.

## Versions an update can install over

RasBeacon never installs an update older than the version it runs (the
same version again is allowed, to repair a system copy). Normally any
older version can install a new update. When a new version can't be
installed over older ones (its settings, partitions or something else
changed in a way the update can't carry), write the oldest version that
can install it into [`image/update-from`](../image/update-from), e.g.
`v2.0.0`, before releasing. The build signs it into the update file:

- A RasBeacon older than that refuses the update file, and its System page
  says the new version **needs a fresh install**: back up the settings,
  flash the new image, set it up and restore the backup. Make sure that
  version can still restore backups from the older ones.
- The online check reads the same from the release's `.update.json` file,
  so owners see it before downloading anything.

Empty `image/update-from` again for the next release unless the reason
still holds; a version that can install `v2.0.0` usually can install
`v2.1.0`, so `v2.1.0` can keep `v2.0.0` there or leave it empty.

## Checklist for a release

1. CI is green on the commit you'll tag.
2. Update `docs/compatibility.md` with any new hardware results.
3. Either tag and push: `git tag -a v0.2.0 -m "RasBeacon v0.2.0" && git push origin v0.2.0`
   (a test version: `v0.2.0-beta.1`), which makes a draft release; or open
   **Actions → Release images → Run workflow** and type the release name,
   which makes the tag and publishes the release at once (a test version as a
   pre-release), so skip the publishing in step 4.
4. When the workflow finishes, open the draft release, check the files are
   all there (the two images, the update file and its `.update.json`, the
   two package lists, `SHA256SUMS`), write the notes, and publish (a test version: tick *Set as a
   pre-release* first).
5. Test the update file on a device running the previous release (System
   page → Install update), and check it comes back on the new version.

## How updates work on the device

See `internal/agent/update.go`. In short: the signature and board are
checked first; the new system is written into the slot that isn't
running; the SHA-256 of everything written must match the signed one;
RasBeacon then restarts into the new slot *on trial*. If it runs normally
for a minute, with the web UI and the hotspot running, the new slot becomes
the default; if not, the device goes
back to the previous version by itself.
