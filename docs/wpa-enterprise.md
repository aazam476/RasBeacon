# Connecting to WPA-Enterprise networks (company, university, eduroam)

Many companies and universities, and eduroam, use **WPA-Enterprise**: you
log in with your own username and password (or a personal certificate),
not a shared Wi-Fi password. RasBeacon supports:

| Login method (in RasBeacon) | Also called | Needs |
|---|---|---|
| EAP-TTLS with PAP | TTLS / PAP | Username and password |
| PEAP with MSCHAPv2 | PEAP | Username and password |
| EAP-TLS | Certificate | Your personal certificate and its key |

Both WPA2-Enterprise and WPA3-Enterprise networks work. RasBeacon uses
whichever the network offers.

## What you need

1. **The login method** (table above). If you don't know it, try
   EAP-TTLS with PAP first, then PEAP.
2. **Your username**, usually your account name with your organization's
   domain, e.g. `user@example.com`.
3. **An anonymous identity** (optional), e.g. `anonymous@example.com`.
   It's sent before the connection is encrypted, so your real username stays
   private. Use the part after `@` from your real username.
4. **Optional, under Advanced:** the CA certificate(s) and the login
   server's name. See below for what happens without them.

## How RasBeacon checks the login server

Anyone can set up a Wi-Fi network with your network's name. If your
device logged in to it, it would hand over your password. So RasBeacon
always checks the login server, in one of three ways:

- **CA certificate and/or server name given** (under **Advanced**): the
  server's certificate must be signed by that CA (or, with only a name, by a
  CA the system trusts) and carry that name. This is the strongest check.
- **Neither given: trust on first connect.** At the first connection,
  RasBeacon only fetches the login server's certificate, without logging in
  (your password isn't sent), and remembers it. From then on it logs in
  only to the server with that exact certificate. Phones and laptops do the
  same when they ask you to "trust this certificate". The Internet page
  shows *Trusted since the first connection* and the start of the
  certificate's fingerprint. Make the first connection somewhere you know
  the network is real, such as your office or campus.
- **"Don't check the login server"** (under **Advanced**, unsafe): for
  testing only. The status page warns for as long as the network is saved.

If a network you trusted at the first connection later shows a different
certificate, RasBeacon doesn't log in, and the status page says so.
Networks replace their certificates every year or two; when that happens,
remove the network on the Internet page and add it again. If it happens
somewhere unexpected, someone nearby may be faking the network.

## Finding the CA certificate and server name

You don't need them (trust on first connect works without), but they make
the check stronger and survive certificate renewals. Places to look:

- **Your organization's Wi-Fi help page.** Look for "manual settings",
  "connect Linux" or "connect other devices". Asking IT for "the RADIUS
  server name and CA certificate" works too.
- **eduroam CAT** ([cat.eduroam.org](https://cat.eduroam.org)), for eduroam:
  pick your institution and download the **Linux** installer. It's a text
  file (a Python script). The CA certificates are in it between
  `-----BEGIN CERTIFICATE-----` and `-----END CERTIFICATE-----` (save each
  as a `.pem` file), and the server names are in a list of names, such as
  `radius.example.edu` (the installer calls them the server names to
  match). This is often the easiest place, since help pages rarely list the
  server name.
- **A device that is already connected.** When a phone or laptop first
  joins the network, it shows the login server's certificate before asking
  you to trust it: the name in it (often called *Domain* or *Server*) is the
  server name. On Android it's the *Domain* field of the network's settings;
  macOS shows the certificate in the "Verify Certificate" prompt (**Show
  Certificate**); Windows shows the server's name in its connection prompt.

A server name can start with `*.`: `*.radius.example.com` accepts
`radius.example.com` and every name ending in `.radius.example.com`.

## Setting it up

1. Open the **Internet** page and tap your network in the *Look for nearby
   networks* list. (Out of range, or hidden? Tap **Add manually**, type the
   network name, and choose **Username and password
   (WPA2/WPA3-Enterprise)** as the security.)
2. Choose the **login method**.
3. Enter your **username**, the **anonymous identity** (optional) and your
   **password**.
4. Optionally, open **Advanced** and choose the **CA certificate file(s)**
   (`.pem`, `.crt`, `.cer` and `.der` all work, several at once) and enter
   the **login server name**.
5. Tap **Add to list**, then **Save**.

The Status page shows *Connected* once the login worked, or *The network
rejected the login* if the username, password, certificate or server name
is wrong.

## Privacy

- Your password and key are stored on the device's data partition, readable
  by the system only, and never shown again in the web UI.
- Logs and the diagnostics report show your username only masked, e.g.
  `u***@example.com`.

## Troubleshooting

| Status page says | What to check |
|---|---|
| *The network rejected the login* | Username (including the `@domain` part), password, login method. If those are right: the CA certificate(s) and server name, if you gave them. |
| *…sent a different certificate than the one RasBeacon trusted…* | The network replaced its certificate (remove the network and add it again), or the network is fake. |
| *Looking for your saved networks* | The network is out of range, or its name isn't exactly the one saved. |
| Connected, but nothing loads | Some networks need you to register the device first, or show a login page (see *Hotel login pages* in the README). |

The Raspberry Pi has no battery-backed clock. RasBeacon saves the time and
sets it again at start, but after being unplugged for a long time the clock
can be behind until it reaches the internet. If it's far enough behind, a
recently issued server certificate looks "not yet valid" and the login
fails; connecting through another network once (Ethernet or a phone
hotspot) sets the clock.
