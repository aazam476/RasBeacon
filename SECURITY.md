# Security policy

RasBeacon is meant to protect your traffic on networks you don't trust,
so security problems matter a lot to us.

## Reporting a problem

**Please don't open a public issue for security problems.** Instead, use
GitHub's private vulnerability reporting: on the repository page, go to
*Security → Report a vulnerability*. Include:

- what you found and how to reproduce it,
- which version (System page, or the image file name) and board,
- what an attacker could do with it.

We'll answer within a week, keep you updated, and credit you in the
release notes if you want.

## What we consider security problems

For example:

- traffic leaving outside the VPN while the kill switch is on (including
  DNS, IPv6, during startup, mode changes or login mode beyond web and
  DNS);
- reaching the web UI or SSH from the upstream network or the VPN;
- getting past the admin login, CSRF protection or login rate limit;
- reading passwords, keys or certificates without the admin password;
- secrets appearing in logs or the diagnostics report;
- installing an update that wasn't signed with the release key;
- a WireGuard file or uploaded certificate that runs commands or changes
  settings it shouldn't;
- guests reaching your devices, other guests, the web UI or SSH, or
  getting outside the VPN while the kill switch is on;
- the zero-leak login browser reaching anything but the uplink (your
  devices, the web UI, the VPN), reading RasBeacon's files, or keeping
  data after it closes;
- a plugin doing more than the permissions it was installed with (reaching
  the internet outside the VPN, your devices, guests, the web UI, other
  files or devices), or being reachable by guests or from the uplink;
- a plugin update accepted that isn't signed by the key of the installed
  version, or a changed `.rba` passing its signature check.

## Supported versions

Only the newest release gets security fixes. Install updates from the
System page (see [docs/releasing.md](docs/releasing.md) for how they are
signed and checked).

## Design notes

How RasBeacon protects things is described in
[docs/design.md](docs/design.md) (sections 3 to 5) and
[docs/architecture.md](docs/architecture.md). The tests that check the
promises above are in `test/netns/` (firewall, VPN leaks, login mode,
plugins), `test/hwsim/` (WPA-Enterprise server checks), and next to the
code. Some of them, for problems found in reviews:

| Promise | Problem it guards against | Tests |
|---|---|---|
| No reading secrets without the admin password | A hotel network's redirect to a `javascript:` address ran as code in the settings page | `internal/portal` `TestProbe` ("script in the Location"), `internal/webserver` `TestSecurityHeaders_AreOnEveryAnswer` (no inline scripts, no framing), `web/test/logic` `loginPageAddress` |
| A plugin does only what its permissions allow | A plugin's `image.tar` naming another plugin's image took it over | `internal/plugin` `TestCheckImageNames_RefusesAnotherPluginsName`, `internal/agent` `TestPlugins_AnImageCarryingAnotherPluginsNameIsRefused` |
| A plugin does only what its permissions allow | Plugins reached each other directly on their shared bridge | `test/netns` `TestPlugins_CantReachEachOther` |
| A plugin does only what its permissions allow | A template that never finishes stopped the other plugins and backups | `internal/plugin` `TestCheckTemplate`, `internal/agent` `TestFillTemplate_RefusesLoops` |
| A plugin does only what its permissions allow | Data kept from a removed plugin went to another author's plugin with the same ID | `internal/agent` `TestPlugins_SigningKeyIsPinnedOnFirstSignedInstall`, `TestPlugins_UnsignedKeptDataComesBackOnlyWhenChosen` |
| No secrets in logs or the diagnostics report | Plugin passwords on podman's command line, so in error messages and the journal | `internal/agent` `TestPlugins_InstallFromAFileStartsItWithItsOwnAddressPortsAndData` (no command holds the password), `TestDiagnostics_HidesPluginPasswords`, `internal/plugin` `TestEnvFileContents` |
