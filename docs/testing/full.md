# Full hardware test plan (everything, end to end)

**Goal:** check every feature of RasBeacon on real hardware, in one pass,
in an order that never makes you redo setup. It brings together the
milestone plans ([m1](m1.md) to [m8](m8.md)) and the later additions:
the scan fix, the hotspot moving on large networks, devices on the Ethernet
port, the card being used in full, swap, your own DNS servers and
DNS-over-HTTPS.

Each check says what should happen. Tick it, or write down what happened
instead. Checks marked **(if you can)** need something you may not have;
skip them freely. The report template at the end has one line per section.

## What you need

| For | What |
|---|---|
| Everything | A Pi 4 or 5, its USB Wi-Fi adapter, a microSD card, power. The newest image from GitHub (Actions → Release images → newest run → Artifacts). Raspberry Pi Imager. |
| Most sections | A phone (A), a second phone or tablet (B), a laptop. Your home router, an Ethernet cable, your home Wi-Fi password. |
| Section 8 | A WireGuard `.conf` for RasBeacon ([server guide](../wireguard-server.md)); a second one for profiles if you can. |
| Section 10 | A network with a login page (hotel, café, library, campus guest). |
| Section 4.6 | Being on campus, with eduroam's certificates ([guide](../eduroam.md)). |
| Section 13 | Two signed builds (blocked until the update signing key is set up, see [releasing.md](../releasing.md)). |
| Optional | A USB power meter, `iperf3` on two computers, a push button and two jumper wires. |

**Pi Zero 2 W:** flash `rasbeacon-zero2w-…` instead, and skip what it
doesn't have: the Ethernet steps (4.1, 4.5, 7), the updates section (13;
the System page should say *New versions are flashed*), and 5 GHz on the
built-in Wi-Fi. In section 3, the Storage total is the card minus about
0.3 GB. Note how slow the login page and speed test are (512 MB of memory).

**SSH** helps with a few checks (marked *SSH*). Turn it on in section 11.1
first if you want them; everything else works from the web pages alone.

---

## 1. Flash and first boot

1. Flash the `.img.xz` (Imager: *Use custom*, **No** to OS customisation).
   Don't create `setup.txt` yet.
2. Plug the adapter into a **USB 3 (blue)** port on a Pi 4 (any port on a
   Pi 5). Power on and note the time.
3. **Expect:** `RasBeacon-Setup-XXXX` appears within about 2 minutes, open
   (no password). Note how long it took.
4. Join it on phone A. **Expect:** the "Log in" sheet (iPhone) or "Sign in
   to network" notification (Android) opens the setup page by itself.
   Otherwise open `http://10.87.0.1`.

## 2. Setup and login

1. Type two different admin passwords. **Expect:** "The two passwords don't
   match."
2. Choose an admin password (10+ characters), keep the hotspot name and
   suggested Wi-Fi password, band **2.4 GHz**, **Finish setup**.
3. **Expect:** within about 10 s the setup network disappears and your
   hotspot appears. Join it with the password.
4. Open `http://10.87.0.1` (or `http://rasbeacon.lan`). Log in with a wrong
   password: **expect** "Wrong password." Then log in correctly **with "Stay
   logged in" on**.
5. **Expect** on Status: *Wi-Fi radios* lists the adapter as the hotspot and
   the built-in Wi-Fi for the uplink; Internet *Not connected*.

## 3. Storage and swap (new)

1. **System → Storage**. **Expect:** the total is the card's size minus
   about 3.5 GB for the two system copies (a 32 GB card shows about 28 GB),
   not about 0.5 GB: the data partition grew to fill the card on the first
   boot.
2. **System → Swap file** row. **Expect:** it says **2 GB**. Tap it: a
   *Size* list (Off, 1 GB, 2 GB, 4 GB, ... and the largest that fits),
   with the largest leaving 1 GB of the card free.
3. Choose **4 GB**, **Save**. **Expect:** the button says *Making the swap
   file…* for up to a minute, then *Saved.*, and the row says 4 GB.
4. Choose **Off**, **Save**, then back to **2 GB**, **Save**.
5. *SSH:* `swapon --show` lists `/persistent/swapfile` at 2G;
   `cat /proc/sys/vm/swappiness` says 10; `df -h /persistent` shows the
   large size; `systemctl status rasbeacon-grow-data` says it ran without
   errors.

## 4. Internet: Ethernet and Wi-Fi uplinks

1. **Ethernet:** plug a cable from your home router into the Pi. **Expect**
   within about 10 s: Status says *Connected by Ethernet* with a
   home-network address; websites load on phone A; no "No internet"
   warning.
2. **Scan (fixed this round):** Internet → **Look for nearby networks**.
   **Expect:** a list of networks within about 5 s, not an error. Your own
   hotspot isn't in it.
3. Tap your home network, type its password, **Add to list**, then **Save**
   in the bar at the bottom. **Expect:** "Saved. RasBeacon is joining the
   networks now".
4. Unplug Ethernet. **Expect:** within about 20 s Status shows
   *Connected to Wi-Fi "…"* with signal and band; websites load.
5. Plug Ethernet back in. **Expect:** it switches back to Ethernet. Unplug
   it again for the next sections.
6. **eduroam (if you can, on campus):** **Set up eduroam**, as in
   [m3](m3.md) step 2 (TTLS-PAP or PEAP, both CA files, server name
   `*.ruw.rutgers.edu`). **Expect:** connected within 30 s, websites load.
   A wrong server name or wrong password: **expect** *The network rejected
   the login* and no connection.
7. **Hotspot moves on large networks (fixed this round, campus/eduroam):**
   if the network you join uses RasBeacon's own address range (10.87.x.x),
   **expect** the hotspot to restart within a minute on another range
   (10.88.0.1, ...), and phone A to reconnect by itself and get internet.
   The settings open at the new address (or `http://rasbeacon.lan`). Leave
   it connected **for at least 15 minutes**, using phone A now and then:
   the internet and the settings pages must keep working the whole time.
8. **Uplink buttons:** **Renew lease** keeps the connection. **Disconnect**
   with two saved networks in range: it joins the other within a minute.
9. **Priority (if you can):** with two networks saved and in range, move
   one up, **Save**. **Expect:** it moves to the top one within a minute.

## 5. Your own DNS servers and DNS-over-HTTPS (new)

The VPN stays **off** for this section.

1. Internet → **DNS servers** row. **Expect:** it says *The network's own*.
2. Tap it, type `dns.google`, **Save**. **Expect:** *Saved.*; the box now
   shows `https://dns.google/dns-query`; the row says *dns.google
   (encrypted)*.
3. On phone A, open <https://dnsleaktest.com> (standard test) or
   <https://one.one.one.one/help>. **Expect:** Google's DNS, not your
   provider's or the network's.
4. Browse a few sites. **Expect:** pages load as fast as before.
5. *SSH:* `systemctl status rasbeacon-doh` is *active (running)*;
   `cat /run/rbd/doh.env` lists the server and `127.0.0.53`;
   `journalctl -u rasbeacon-doh -b` shows no errors.
6. Type `https://cloudflare-dns.com/dns-query` and `1.1.1.1` on two lines,
   **Save**. **Expect:** the leak test now shows Cloudflare.
7. Type `not a server`, **Save**. **Expect:** an error under the box, and
   nothing changes.
8. Plain DNS only: `9.9.9.9`, **Save**. **Expect:** the leak test shows
   Quad9; *SSH:* `systemctl status rasbeacon-doh` is *inactive*.
9. Empty the box, **Save**. **Expect:** *The network's own* again.
10. On the laptop: `nslookup example.com 8.8.8.8`. **Expect:** an answer
    (RasBeacon answers every DNS question itself).

## 6. Devices and the Clients page

1. Connect phone B and the laptop to the hotspot. **Expect:** Status shows
   *Connected devices (N)* with the right count; phone A is marked *this
   device* on Clients.
2. Start a big download on phone B. **Expect:** Clients and Status →
   Traffic show a rate close to what phone B shows, on the right band.
3. Clients → turn off **Internet access for** phone B. **Expect:** within
   a few seconds phone B stops loading websites (including a running
   download) but can still open `http://10.87.0.1`. The *Paused (1)* filter
   shows it. Turn it back on: it works at once.
4. **Disconnect** phone B. **Expect:** it drops off Wi-Fi and usually
   rejoins within a minute.
5. **Flush lease** on phone B. **Expect:** no error; it gets an address
   again when it next joins.

## 7. The Ethernet port as a client port

1. Internet → Ethernet port **Client port**, **Save** (keep the Wi-Fi
   uplink connected).
2. Plug the laptop into the Pi's Ethernet port, laptop Wi-Fi off.
   **Expect:** a `10.87.0.x` address, internet, and it can ping phone A.
3. **New:** Clients lists the laptop with a **cable icon** and
   *Connection: Ethernet cable*, and *Traffic: Not measured*. There is no
   Disconnect button for it. Pausing its internet access works (step 6.3).
   On Status, it is counted among the connected devices.
4. Unplug the laptop. **Expect:** it leaves the list within about a minute.
5. **Safety check:** plug the Pi's Ethernet port into your **home router**
   (still *Client port*). **Expect:** a warning that the port "seems to be
   plugged into a network", and other devices at home keep getting normal
   addresses.
6. Set the port back to **Uplink**, **Save**.

## 8. VPN and kill switch

1. VPN → choose your `.conf` → **Import**. **Expect:** "WireGuard file
   imported", the profile `….conf` is picked.
2. Turn on **Use the VPN** (kill switch on). **Expect:** within about 10 s
   Status says *Connected: your devices' traffic goes through the VPN*.
   <https://ifconfig.me> on phone A shows the VPN server's address.
3. **DNS:** the leak test shows your VPN's DNS. Set your own DNS servers
   (section 5) to `dns.google` while the VPN is on: **expect** the leak
   test still shows the VPN's DNS (the VPN's DNS wins), and *SSH:*
   `rasbeacon-doh` is inactive. Empty the DNS box again.
4. **Kill switch on:** break the tunnel (stop the server, or change the
   port in *Server details* to a wrong one, **Save**). After 3 to 4
   minutes, **expect** *Not connected. The kill switch is on…*, and no
   website loads on phone A. Fix it: **expect** websites back within a
   minute or two.
5. **Kill switch off:** untick it, break the tunnel again. **Expect** after
   about 4 minutes *using the internet WITHOUT the VPN* and websites load
   from your home address. Fix it: back to *Connected*. Tick the kill switch
   again.
6. **Boot with the VPN broken:** break the tunnel, pull the power, plug it
   back in, and try websites on phone A for 2 minutes. **Expect:** nothing
   loads. Fix the tunnel: phone A gets online.
7. **Server details:** type `big` as the MTU, **Save**: **expect** "Must be
   a number, or empty for the default." Type `1420`, **Save**: *Saved.*
8. **Profiles (if you have a second file):** import it, pick it. **Expect**
   Status connected again within about 30 s and ifconfig.me showing the
   second server. While switching, a `ping 1.1.1.1` on the laptop may pause
   but must never show your home address. Remove the second profile:
   **expect** it falls back to the first.
9. **Clock:** unplug the Pi for 10+ minutes, plug it in. **Expect** the VPN
   reconnects by itself within a couple of minutes.

## 9. Speed test

1. Internet → **Run speed test**. **Expect:** it finishes in 20 to 40 s with
   download, upload, ping and a Cloudflare location, and the hotspot stays
   usable meanwhile.
2. Compare with <https://speed.cloudflare.com> on phone A. RasBeacon
   measures its own connection (not the VPN), so it should be close to or
   above the phone's.

## 10. Hotel login pages (needs a network with a login page)

Keep the VPN on with the kill switch.

1. Join the network (Internet page). **Expect** within 2 minutes: Status
   says *This network shows a login page*; phone A shows *This network
   needs a login* by itself (or open `http://10.87.0.1/#/portal`).
2. If you set your own DNS servers (section 5), **expect** the network's
   DNS to be used while the login page is in the way (the login page still
   loads).
3. **Zero-leak login:** tap **Open the login page here**. **Expect** the
   login page as a picture within about 10 s. Tap fields, type into the box
   under the picture and **Type it**, scroll by swiping. Log in. **Expect**
   the picture to go away, *Nothing to do*, and the VPN to connect. Note how
   long a tap takes to show.
4. **Leak check:** on the laptop, before logging in, `ping 8.8.8.8` and
   websites must fail until the VPN is up.
5. **Login mode (if the network lets you log out, or another network):**
   **Start login mode**, log in on the phone's own browser. **Expect** the
   VPN connected and login mode off afterwards, and it ending by itself
   after 10 minutes if you don't log in.

## 11. Settings pages (phone and laptop)

1. **SSH:** paste your laptop's public key, turn SSH on, **Save**.
   `ssh root@10.87.0.1` works. (Leave on for the *SSH* checks, then turn
   off.)
2. **Every page, on phone A and on the laptop (window 1024+ px wide), in
   light and dark mode:** Status, Internet, VPN, Clients, and Hotspot,
   Guests, SSH, HTTPS, System (under *More* on the phone, in the sidebar on
   the laptop). **Expect:** nothing cut off, overlapping or unresponsive;
   typing into a box never zooms the page on the iPhone.
3. **Focus outline (fixed this round):** on the laptop, press **Tab**
   through a page's text boxes and switches. **Expect:** the outline around
   each follows its rounded corners (no square corner cut off).
4. **Readings:** System → Hardware temperature and memory are about right
   (*SSH:* `vcgencmd measure_temp`, `free -m`); *Slowed down for heat* is
   *No* on a good power supply.
5. **Stay logged in:** after the next reboot you are still logged in.
   **Log out**, log in *without* it, reboot: you must log in again.
6. **Unsaved changes:** on the Internet page, add a network but don't save,
   **expect** the bar *You have changes that are not saved yet.*;
   **Discard changes** removes it.

## 12. Hotspot, guests and other settings

1. **Hotspot:** a new Wi-Fi password `short` → **Save**: "Must be 8 to 63
   characters long." Leave it empty and set band **5 GHz** → **Save**:
   *Saved.*, the hotspot comes back on 5 GHz, devices rejoin. Switch back to
   2.4 GHz if you like.
2. **Show Wi-Fi password** with the admin password: shows the current one.
3. **Metered:** tick it, **Save**: devices do **not** disconnect; Status
   shows *Metered connection On*; Android (after Wi-Fi off/on) treats the
   network as metered. Untick it.
4. **Guest network:** if the Guests page says the adapter can only
   broadcast one network, note it and skip. Otherwise turn it on with a
   name and password (without a password: "Choose a password for the guest
   network."). **Expect:** *Saved. The hotspot has restarted.*; phone B on
   the guest network has internet (through the VPN if on), can't open
   `http://10.87.0.1` or `http://10.87.1.1`, and can't see or ping phone A;
   Status shows the guest network running with 1 guest.
5. **Public IPv6 (if your WireGuard server routes a /64):** see
   [m7](m7.md) step 3. Otherwise check <https://test-ipv6.com> loads quickly
   and Status shows whether devices get IPv6.
6. **HTTPS:** turn on **Use HTTPS for these settings pages**. Open
   `https://10.87.0.1`: the fingerprint in the browser's warning matches
   the page's *SHA-256 fingerprint*; `http://` redirects to `https://`.
   Turn it off again if you prefer.

## 13. Updates (blocked until the signing key is set up)

Today's images can't install updates (System says so). Once a release is
built with the update signing key ([releasing.md](../releasing.md)), flash
that release and run [m6](m6.md) steps 1 to 3: a newer `.rbupdate`
installs and keeps all settings; a renamed image, an update made for another board
(`rbsign pack -board other`) and a file with one byte changed are refused without restarting; a power cut
during the restart rolls back to the old version.

## 14. Robustness

1. **Power cuts:** three times, pull the power while running, wait 5 s,
   plug back in. **Expect** each time: same hotspot, same settings, the
   uplink (and VPN) back within about a minute, still logged in.
2. **Reboot:** System → **Reboot** asks first (**Cancel** does nothing);
   confirming restarts.
3. **Long run:** leave 3+ devices connected and in use for an hour (or as
   many as you have: the goal from [m1](m1.md) is 10 for 30 minutes).
   **Expect:** no drops, the hotspot stays up, devices rejoin.
4. **Memory (on a 1 GB board if you have one):** open the zero-leak login
   (section 10) or run the speed test, then *SSH:* `free -m`. **Expect:**
   it works; swap used stays small.
5. **Diagnostics:** System → **Download diagnostics**. Search it for your
   hotspot password, Wi-Fi passwords, the VPN's `PrivateKey`, your eduroam
   username. **Expect:** none appear (usernames only masked, like
   `N***@…`).

## 15. Erasing (do these last)

1. **Factory reset:** System → *Factory reset* → **Erase everything** with
   the admin password. **Expect:** it restarts into `RasBeacon-Setup-XXXX`.
2. **setup.txt:** power off, create `setup.txt` on the card's `BOOTCONFIG`
   drive with `setup_password=testing-1234`, boot. **Expect:** the setup
   network asks for that password. Then change it to
   `setup_password=short`, boot: **expect** no setup network and the green
   LED blinking 3 short, 1 long. Delete the file, boot, finish setup:
   `setup.txt` is gone from the card afterwards.
3. **factory-reset file:** power off, create an empty file
   `factory-reset` (no extension) on `BOOTCONFIG`, boot. **Expect:** the
   setup network, and the file gone afterwards.
4. **Reset button (if you can):** button between pin 11 (GPIO 17) and pin 9
   (ground). A short press does nothing; holding 10 s erases everything
   like step 1.

## 16. Measurements (if you can)

1. **Power:** a USB meter between supply and Pi: watts idle with 5 devices,
   and during a download. With a power bank: does it stay on after 30
   minutes idle?
2. **Throughput:** `iperf3` between two laptops on the hotspot, and from a
   hotspot laptop to a home computer, as in [m1](m1.md) step 4 and
   [m2](m2.md) step 8.

---

## Report template

Copy this, fill it in (yes/no, numbers, or what went wrong), and paste it
back. One line per section; add details under any that failed.

```
Board / RAM:            Adapter:            Card size:          Image:
1  First boot: setup network after __ s; opened by itself iPhone/Android:
2  Setup + login (mismatch caught, wrong password refused, stay logged in):
3  Storage shows full card (__ GB): / swap row 2 GB: / 4 GB, Off, 2 GB save: / SSH swapon+swappiness:
4  Ethernet: / scan lists networks: / Wi-Fi uplink + switch: / eduroam: / hotspot move kept working 15 min: / renew, disconnect:
5  dns.google saved as full address: / leak test shows Google: / rbdoh running: / Cloudflare+1.1.1.1: / bad entry refused: / plain only, rbdoh stopped: / back to network's:
6  Count + this device: / traffic rates: / pause + reboot-proof: / disconnect: / flush lease:
7  Laptop on client port gets address+internet: / listed with cable icon, no Disconnect: / leaves list: / safety warning:
8  Import + connect (IP = VPN): / DNS = VPN's even with own DNS set: / kill switch on no leak: / off falls back: / boot no leak: / MTU check: / profiles: / clock:
9  Speed test: down __ up __ ping __ location __, took __ s / speed.cloudflare.com: down __ up __
10 Login page notice: / zero-leak login worked, tap delay __ s: / no leak: / login mode:
11 SSH: / pages OK phone+laptop+dark: / rounded focus outline: / readings: / stay logged in: / unsaved bar:
12 Hotspot password check + 5 GHz: / show password: / metered: / guests isolated: / IPv6: / HTTPS fingerprint + redirect:
13 Updates: blocked (no signing key yet) / results:
14 Power cuts x3: / reboot dialog: / long run __ devices __ min, drops: / memory: / diagnostics clean:
15 Factory reset: / setup.txt password + broken-file LED + deleted: / factory-reset file: / button:
16 Power idle __ W, load __ W / iperf3:
Anything else odd:
```
