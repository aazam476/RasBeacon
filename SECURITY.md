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
  data after it closes.

## Supported versions

Only the newest release gets security fixes. Install updates from the
System page (see [docs/releasing.md](docs/releasing.md) for how they are
signed and checked).

## Design notes

How RasBeacon protects things is described in
[docs/design.md](docs/design.md) (sections 3 to 5) and
[docs/architecture.md](docs/architecture.md). The tests that check the
promises above are in `test/netns/` (firewall, VPN leaks, login mode) and
`test/hwsim/` (WPA-Enterprise server checks).
