# Glossary

Networking and security terms used in RasBeacon's code and docs, in plain
words.

**Access point (AP), hotspot**
: A Wi-Fi radio that other devices join. RasBeacon's USB adapter is an access
point, run by the program *hostapd*.

**A/B update**
: Keeping two copies of the operating system (slot A and slot B). An update
  is written to the unused copy and tried once. If it fails to start
  properly, the device goes back to the old copy automatically.

**argon2id**
: A slow, memory-hungry *hash* function designed for passwords, so guessing
  passwords from a stolen hash is expensive.

**Band (2.4 GHz / 5 GHz)**
: The two main Wi-Fi frequency ranges. 2.4 GHz reaches further and every
  device supports it; 5 GHz is faster but has shorter range.

**Bridge (br-lan)**
: A virtual network switch inside Linux. The hotspot radio (and, later, a
  wired client port) plug into it, so all your devices share one network,
  10.87.0.0/24.

**Captive portal**
: A web page a network makes you visit before it lets you online (hotel
  login pages). Phones detect them by trying to load a known page, such as
  `captive.apple.com`, and show a "Log in to network" screen. RasBeacon uses
  the same trick on purpose during setup, and must get through other
  networks' portals later (milestone 5).

**Channel**
: A specific frequency within a *band*. Nearby networks on the same or
  overlapping channels share airtime.

**conntrack**
: The part of the Linux firewall that remembers ongoing connections, so
  replies can be let back in ("established" in the firewall rules).

**CSRF (cross-site request forgery)**
: An attack where a malicious website makes your browser send a request to
  another site you are logged into. RasBeacon blocks it with SameSite cookies
  and a token that must be sent in a header.

**DFS (dynamic frequency selection)**
: Some 5 GHz channels are shared with radar. An access point on them must
  listen for radar and move away. RasBeacon avoids these channels because USB
  adapters aren't verified to do this correctly.

**DHCP**
: How a device gets its IP address when it joins a network. On the hotspot,
  dnsmasq is the DHCP server.

**DNS**
: Turns names like `rasbeacon.lan` into IP addresses. On the hotspot,
  dnsmasq answers DNS questions.

**Forwarding (routing)**
: Passing packets from one network interface to another, for example from
  the hotspot to the uplink. Linux only does it when switched on; RasBeacon
  switches it on after loading its firewall rules.

**Firewall, nftables**
: Rules that decide which network packets may reach the device or pass
  through it. RasBeacon uses Linux's *nftables*, and every rule is commented
  in `internal/render/templates/firewall.nft.tmpl`.

**Hash**
: A one-way function: easy to compute from the input, practically impossible
  to reverse. Passwords are stored as hashes.

**Handshake (WireGuard)**
: The short exchange in which the device and the server agree on fresh
  keys. It happens every 2 minutes while traffic flows; RasBeacon treats a
  handshake older than 3 minutes as "the tunnel is not working".

**Kill switch**
: When on, your devices' traffic may only leave through the VPN tunnel. If
  the tunnel is down, traffic is blocked rather than sent unprotected.

**Policy routing, routing table**
: Linux can keep several routing tables and choose one by rules (for
  example "traffic from the hotspot uses table 51820"). RasBeacon uses
  this to send devices' traffic into the VPN while its own traffic uses
  the uplink.

**Masquerade, NAT66**
: *Masquerade* is the kind of NAT RasBeacon uses: outgoing traffic gets the
  uplink's current address as its sender. *NAT66* is the same for IPv6.

**Metric (route metric)**
: A route's cost. When two routes lead to the internet, Linux uses the one
  with the lower metric. Ethernet's is 100, Wi-Fi's 600, so a cable wins.

**MAC address, private (randomized) MAC**
: A network card's hardware address. Phones now invent a different one per
  network (and may change it over time) for privacy.

**NAT**
: Network address translation. Many devices share one upstream address; the
  router rewrites addresses on the way out and back in.

**Network namespace (netns)**
: An isolated copy of Linux's network stack that acts like a separate
  computer. RasBeacon's tests build small virtual networks out of them.

**MSS clamping**
: Making new TCP connections agree on packets small enough for every link
  on the way. Without it, some websites hang on networks with smaller
  packets (tunnels, some hotel networks).

**PMF (protected management frames, 802.11w)**
: Encryption for Wi-Fi control messages, so attackers can't easily kick
  devices off. Required by WPA3.

**Router advertisement (RA)**
: An IPv6 message in which a router says "here is a network prefix, and
  (optionally) you can reach the internet through me". A *router lifetime*
  of 0 means "not through me".

**Regulatory country**
: Which country's radio rules apply: allowed channels and transmit power.

**rfkill**
: Linux's software on/off switch for radios. Some Raspberry Pi images start
  with Wi-Fi switched off.

**SAE**
: The password method used by WPA3-Personal. It resists offline password
  guessing better than WPA2's method.

**SSID**
: A Wi-Fi network's name.

**Guest network**
: A second Wi-Fi network on RasBeacon for visitors. Guests get internet
  access but can't reach your devices or the settings (milestone 7).

**Metered connection**
: An internet connection that costs per megabyte or is limited, like a
  phone's hotspot. Devices told about it hold back big downloads.

**Zero-leak login**
: Logging in to a hotel's login page from a browser running on RasBeacon
  itself, shown on your phone as pictures, so none of your devices' traffic
  leaves the VPN (milestone 7).

**ULA (unique local address)**
: A private IPv6 address range (starting with `fd`) that each RasBeacon
  picks at random, like 10.x.x.x is for IPv4.

**Uplink**
: The network RasBeacon itself connects to for internet: hotel Wi-Fi,
  eduroam or Ethernet (milestone 2).

**WireGuard**
: A modern VPN protocol. RasBeacon will send your traffic through your own
  WireGuard server (milestone 4).

**WPA2 / WPA3**
: Wi-Fi password protection standards. WPA3 is newer and stronger;
  "transition mode" lets both kinds of device join.
