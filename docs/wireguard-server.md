# Setting up a WireGuard server for RasBeacon

RasBeacon connects to **one** WireGuard server and sends all of your
devices' traffic through it. You can use:

- **A VPN provider** that offers WireGuard configuration files (look for
  "WireGuard configuration" or "manual setup" in your account). Download a
  `.conf` file and import it on RasBeacon's **VPN** page.
- **Your own server**: a home router with WireGuard (many OpenWrt, GL.iNet,
  UniFi and FRITZ!Box models), or a small cloud server. This guide covers a
  Debian/Ubuntu server.

## What RasBeacon needs in the file

RasBeacon reads standard WireGuard (`wg-quick`) files, with these rules:

| Setting | Rule |
|---|---|
| `[Interface] PrivateKey` | Required. |
| `[Interface] Address` | Required: RasBeacon's address(es) inside the tunnel. Add an IPv6 address to give your devices IPv6 through the VPN. |
| `[Interface] DNS` | **Required**: a DNS server reached through the tunnel. Without it, name lookups would bypass the VPN. |
| `[Interface] MTU` | Optional; RasBeacon uses 1380 by default, which works on most hotel networks. |
| `[Peer] AllowedIPs` | Must include `0.0.0.0/0` (and `::/0` for IPv6): everything goes through the tunnel. Split tunnels are not supported. |
| `[Peer] PersistentKeepalive` | Optional; RasBeacon uses 25 seconds if it's missing. |
| One `[Peer]` only | RasBeacon connects to one server. |
| `PostUp`, `PreUp`, `PostDown`, `PreDown`, `Table`, `FwMark`, `SaveConfig` | **Refused.** RasBeacon never runs commands from a file and sets up routing itself. Delete these lines. |

## Your own server (Debian or Ubuntu)

These steps make a server at `vpn.example.com` (use your server's name or
public IP address) with the tunnel network `10.66.0.0/24`.

1. **Install WireGuard** on the server:
   ```sh
   sudo apt install wireguard
   ```
2. **Make the server's keys:**
   ```sh
   wg genkey | sudo tee /etc/wireguard/server.key | wg pubkey | sudo tee /etc/wireguard/server.pub
   ```
3. **Get RasBeacon's public key:** on RasBeacon's **VPN** page, open
   *Server details*, tap **Generate new key**, and copy *RasBeacon's
   public key*. (Or, if you'll import a file you write yourself, run
   `wg genkey` on your computer and put the private key in the file.)
4. **Configure the server**, `/etc/wireguard/wg0.conf` (replace `eth0` with
   the server's internet interface, see `ip route show default`):
   ```ini
   [Interface]
   Address = 10.66.0.1/24
   ListenPort = 51820
   PrivateKey = <contents of /etc/wireguard/server.key>
   # Let RasBeacon's traffic out to the internet.
   PostUp = sysctl -w net.ipv4.ip_forward=1
   PostUp = iptables -t nat -A POSTROUTING -s 10.66.0.0/24 -o eth0 -j MASQUERADE
   PostDown = iptables -t nat -D POSTROUTING -s 10.66.0.0/24 -o eth0 -j MASQUERADE

   [Peer]
   # RasBeacon
   PublicKey = <RasBeacon's public key>
   AllowedIPs = 10.66.0.2/32
   ```
   (`PostUp` is fine **on the server**; it's only refused in the file you
   give RasBeacon.)
5. **A DNS server for the tunnel.** The simplest is a public resolver that
   RasBeacon reaches *through* the tunnel, such as `1.1.1.1` or `9.9.9.9`.
   If you want your own, install `dnsmasq` or `unbound` on the server and
   let it listen on `10.66.0.1`.
6. **Start it** and open UDP port 51820 in the server's firewall:
   ```sh
   sudo systemctl enable --now wg-quick@wg0
   ```
7. **The file for RasBeacon** (paste this on the VPN page; skip
   `PrivateKey` if you generated the key on RasBeacon in step 3, and use
   *Server details* instead of importing):
   ```ini
   [Interface]
   PrivateKey = <RasBeacon's private key, if you made it with wg genkey>
   Address = 10.66.0.2/32
   DNS = 1.1.1.1

   [Peer]
   PublicKey = <contents of /etc/wireguard/server.pub>
   Endpoint = vpn.example.com:51820
   AllowedIPs = 0.0.0.0/0, ::/0
   PersistentKeepalive = 25
   ```

## Checking that it works

- RasBeacon's **Status** page should say *Connected: your devices' traffic
  goes through the VPN* and show a recent handshake.
- On the server, `sudo wg show` lists RasBeacon's "latest handshake".
- From a phone on RasBeacon's hotspot, a "what is my IP" site should show
  the **server's** address.

## The kill switch

On the VPN page, **Kill switch** is on by default:

- **On:** if the tunnel stops working, your devices have no internet at all
  until it works again. Nothing is ever sent outside the VPN, not even DNS
  lookups.
- **Off:** if the tunnel has been down for 30 seconds, your devices use
  the internet directly (the Status page warns about this) and go back to
  the VPN once it has worked again for a while.

Hotel login pages and the kill switch: when a hotel's login page is in
the way, the Status page offers to open it (for a few minutes, or in
RasBeacon's own browser), and the kill switch stays on the whole time.
