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
- installing an update that wasn't signed with the release key, or one
  older than the version running (a beta is older than its release; a
  test build installs only over a test build);
- a release's `SHA256SUMS` that its signature (`SHA256SUMS.sig`) accepts
  but the release workflow didn't sign;
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
  version, or a changed `.rba` passing its signature check;
- a network RasBeacon joins leaving something behind that breaks it after
  you've left (a wrong time that is saved, say).

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
| A plugin does only what its permissions allow | A plugin's `image.tar` held `manifest.json` in a way Podman reads and RasBeacon's check didn't (`././manifest.json`, a link, a second copy), so another plugin's image name got past the check | `internal/plugin` `TestImageNames_ReadsTheNamesPodmanWould`, `TestImageNames_RefusesLinksThatCouldChangeTheNames`, `internal/agent` `TestPlugins_AnImageThatTakesAnotherPluginsNameIsPutBack` |
| A plugin does only what its permissions allow | Plugins ran as the Pi's root (rootful Podman, no user namespace): a way out of the container was root on the Pi | `internal/plugin` `TestRunArgs_AUserNamespaceOfItsOwn`, `internal/quotafs` `TestMount_ShowsOwnersAsThePluginsUserNamespaceSeesThem`, `internal/agent` `TestShares_ReachThePluginsUserNamespace`, `TestGrantDevice` |
| A plugin does only what its permissions allow | Plugins reached each other directly on their shared bridge | `test/netns` `TestPlugins_CantReachEachOther` |
| A plugin does only what its permissions allow | A plugin's traffic for the hotel's own network is routed to the main table (rule 102), so only the firewall keeps it from leaving outside the VPN; RasBeacon's downloads for plugins must take the tunnel too | `test/netns` `TestPlugins_ReachTheInternetOnlyThroughTheVPN` |
| A plugin does only what its permissions allow | A template that never finishes stopped the other plugins and backups | `internal/plugin` `TestCheckTemplate`, `internal/agent` `TestFillTemplate_RefusesLoops` |
| A plugin does only what its permissions allow | Data kept from a removed plugin went to another author's plugin with the same ID | `internal/agent` `TestPlugins_SigningKeyIsPinnedOnFirstSignedInstall`, `TestPlugins_UnsignedKeptDataComesBackOnlyWhenChosen` |
| A plugin does only what its permissions allow | A template built a value of gigabytes with variables or `printf`, and rbd ran out of memory at every start | `internal/plugin` `TestCheckTemplate`, `internal/agent` `TestFillTemplate_RefusesHugeValues` |
| Only signed updates, never an older version | A beta counted as its release (only the numbers were compared), and a test build has no number, so a beta's or a test build's update file installed over a release | `internal/version` `TestCompare`, `internal/update` `TestCheckVersion` |
| Only signed updates, never an older version | Signing a release's `SHA256SUMS`, which the image build writes, with the update key could have signed anything that build put in it, a forged update's manifest among them | `cmd/rbsign` `TestSignChecksums_SignsOnlyAListOfChecksums`, `TestSignChecksums_SshKeygenChecksIt` |
| No traffic outside the VPN while the kill switch is on | In login mode, connections opened earlier through the tunnel carried on through the hotel's network (routed IPv6, which isn't NATed) | `test/netns` `TestLoginMode_ConnectionsOpenedThroughTheVPNDontLeaveOutsideIt`, `TestLoginMode_RoutedIPv6ConnectionsDontLeaveOutsideTheVPN` |
| Guests can't reach your devices | A guest sending with one of your devices' addresses as the sender got the answers from the internet delivered to that device | `test/netns` `TestGuest_CantSendWithADevicesAddress`, `internal/render` `TestFirewall_GuestNetworkIsolated` |
| A plugin does only what its permissions allow | With the VPN on, a device's connection to a plugin's port was routed into the tunnel (to the VPN server) instead of to the plugin | `test/netns` `TestPlugins_DevicesReachTheirPortsWhileTheVPNIsOn` |
| A plugin does only what its permissions allow | rbd, as root, followed links in a share's files, which the plugin writes (and on a USB drive anyone who had it in a computer): encrypting or decrypting a share, deleting it, or holding its room on the drive could write, cut short or delete files anywhere on the Pi, and a link for a share's folder made another folder the share | `internal/agent` `TestShareLinks_*`, `internal/quotafs` `TestReservation_FollowsNoLink`, `internal/plugin` `TestParseDrives` (drives are mounted `nosymfollow`) |
| No reading secrets without the admin password | A restored backup's settings weren't checked like the settings pages check them, so a changed backup could put text of its own into hostapd's, dnsmasq's or the firewall's files | `internal/agent` `TestBackup_RestoreRefusesSettingsItsPagesWouldRefuse` |
| No reading secrets without the admin password | Any device on the hotspot could watch the login browser another device opened, and type into it: what was typed into a hotel's login page (a room number, a name, a card) wasn't the typing device's alone | `internal/agent` `TestPortalBrowser_OnlyTheDeviceThatOpenedIt`, `internal/webserver` `TestPortalBrowser_TheOpeningDeviceGetsASecret`, the browser walkthrough's zero-leak step |
| A network leaves nothing behind that breaks RasBeacon later | The clock was set by NTP, from the time server the network names: a time years ahead was saved for every later boot, and the VPN's server then refused handshakes with the right time | `internal/agent` `TestClock_TakesNoTimeFromNTP`, `TestClock_AClockThatIsAheadIsPutBack`, `internal/clock` `TestFetchDate_TheTimeIsOneTheCertificateIsValidAt` |
| No traffic outside the VPN while the kill switch is on | Guests' traffic was only checked without the VPN | `test/netns` `TestGuest_UsesTheVPNAndTheKillSwitch` |
| No getting past the admin login | A second setup in the seconds before the network switch replaced the owner's password | `internal/agent` `TestClaim_SecondClaimBeforeTheSwitchIsRejected` |
| No secrets in logs or the diagnostics report | A mobile modem's carrier password is on mmcli's command line, which a failed connection's error named: it was shown on the Internet page and logged | `internal/agent` `TestModem_AFailedConnectionDoesntShowThePassword` |
| No secrets in logs or the diagnostics report | Plugin passwords on podman's command line, so in error messages and the journal | `internal/agent` `TestPlugins_InstallFromAFileStartsItWithItsOwnAddressPortsAndData` (no command holds the password), `TestDiagnostics_HidesPluginPasswords`, `internal/plugin` `TestEnvFileContents` |
