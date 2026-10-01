# Connecting to eduroam and other WPA-Enterprise networks

Universities, many companies and eduroam use **WPA-Enterprise**: you log
in with your own username and password (or a personal certificate), not a
shared Wi-Fi password. RasBeacon supports:

| Login method (in RasBeacon) | Also called | Needs |
|---|---|---|
| EAP-TTLS with PAP | TTLS / PAP | Username and password |
| PEAP with MSCHAPv2 | PEAP | Username and password |
| EAP-TLS | Certificate | Your personal certificate and its key |

Both WPA2-Enterprise and WPA3-Enterprise networks work. RasBeacon uses
whichever the network offers.

## What you need from your institution

1. **The login method** (table above).
2. **Your username**, usually your account name with your institution's
   domain, e.g. `NetID@rutgers.edu`.
3. **An anonymous username** (optional), e.g. `anonymous@rutgers.edu`.
   It's sent before the connection is encrypted, so your real username stays
   private. Use the part after `@` from your real username.
4. **The CA certificate(s)**: the certificate authority that signed the
   login server's certificate. Some institutions list more than one; choose
   all of them.
5. **The login server's name**, e.g. `*.ruw.rutgers.edu` or
   `radius.example.edu`.

**Where to find them:** your institution's eduroam or Wi-Fi help page
("connect Linux", "connect other devices", or "manual settings"). Many
institutions also publish their settings through
[eduroam CAT](https://cat.eduroam.org): pick your institution and download
the **Linux** installer. It's a text file (Python script) that contains the
CA certificates (between `-----BEGIN CERTIFICATE-----` and
`-----END CERTIFICATE-----`) and the server names.

## Why RasBeacon insists on the CA certificate and server name

Anyone can set up a Wi-Fi network called "eduroam". Without checking the
login server's certificate, your device would send your password to that
fake network. The CA certificate proves the server's certificate was issued
by the right authority, and the **server name** makes sure it was issued *to
your institution*. Public authorities like USERTrust sign certificates for
millions of servers, so the CA alone isn't enough.

So RasBeacon won't save an Enterprise network without both. If you really
need to skip the check (for example to test), open **Advanced** and tick
"I understand the risk". The status page then shows a warning for as long
as the network is saved.

## Setting it up

1. Open the **Internet** page and tap **Set up eduroam** (or tap your
   network in the *Look for nearby networks* list).
2. Choose the **login method**.
3. Enter your **username**, the **anonymous username** (optional) and your
   **password**.
4. Under **CA certificate file**, choose the certificate file(s). `.pem`,
   `.crt`, `.cer` and `.der` all work, and you can choose several at once.
5. Enter the **login server name**.
6. Tap **Add to list**, then **Save**.

The Status page shows *Connected* once the login worked, or *The network
rejected the login* if the username, password, certificate or server name
is wrong.

## Example: Rutgers University

These are the values for Rutgers' eduroam as provided by RasBeacon's owner.
They are only an example: check
[Rutgers OIT's eduroam instructions](https://oit.rutgers.edu) for the
current ones, since institutions change certificates and servers over time.

| Field | Rutgers value |
|---|---|
| Network | `eduroam` |
| Username | `NetID@rutgers.edu` (your NetID) |
| Anonymous username | `anonymous@rutgers.edu` (optional) |
| CA certificates | **USERTrust RSA Certification Authority** and **Rutgers Device Root CA** (choose both files) |
| Login server name | `*.ruw.rutgers.edu` |
| Login method | As published by Rutgers OIT. If unsure, try EAP-TTLS with PAP first, then PEAP. |

## Privacy

- Your password and key are stored on the device's data partition, readable
  by the system only, and never shown again in the web UI.
- Logs and the diagnostics report show your username only masked, e.g.
  `N***@rutgers.edu`.

## Troubleshooting

| Status page says | What to check |
|---|---|
| *The network rejected the login* | Username (including the `@domain` part), password, login method. If those are right: the CA certificate(s) and server name. |
| *Looking for your saved networks* | The network is out of range, or the network name isn't exactly `eduroam`. |
| Connected, but nothing loads | Some campuses need you to register the device first, or show a login page; hotel-style login pages are supported from milestone 5. |

One known limitation: the Raspberry Pi has no battery-backed clock, so
after being unplugged for a long time its clock can be behind. If it's far
enough behind, a recently issued server certificate looks "not yet valid"
and the login fails. Connecting through another network once (Ethernet or a
phone hotspot) sets the clock. A saved clock that survives power-off is
planned for milestone 4.
