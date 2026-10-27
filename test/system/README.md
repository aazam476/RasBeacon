# Full-system test

The other tests check RasBeacon in parts: the firewall and dnsmasq in
network namespaces (`test/netns`), hostapd and wpa_supplicant with simulated
radios (`test/hwsim`), and the web UI against a simulated device
(`test/e2e`). This one runs **the whole of RasBeacon**: the real `rbd` and
`rbweb`, started by systemd from the image's own service files, controlling
real hostapd, wpa_supplicant, dnsmasq, nftables and WireGuard, with
simulated Wi-Fi radios (`mac80211_hwsim`).

## The world it builds

Everything runs in one virtual machine, a Debian trixie system with the
image's packages. At boot, before `rbd` starts, the test makes six simulated
radios. RasBeacon keeps two (`wlan0` runs the hotspot, `wlan1` joins the
hotel); the others move into their own network namespaces:

| Namespace | Radio | What it is |
| --- | --- | --- |
| `hotel` | `wlan2` | An open hotel Wi-Fi ("Grand Hotel") with DHCP and DNS, a login page that every web request goes to until the terms are accepted, "the internet" (`203.0.113.1`, which tells each visitor its address on `/whoami`), and a WireGuard server. |
| `phone1` | `wlan3` | The owner's phone. |
| `phone2` | `wlan4` | A guest's phone. |
| `phone3` | `wlan5` | A phone that isn't on the allow list, and later joins with WPA3. |

## The scenarios

They run in order, each building on the ones before (the first failure
stops the run):

1. A phone joins the open setup network and sets RasBeacon up (through
   `rbweb`, like the setup page), then joins the new hotspot, which runs
   Wi-Fi 6 by itself (the simulated radios can).
2. RasBeacon joins the hotel's Wi-Fi, notices the login page, and the owner
   accepts the terms in RasBeacon's own login browser. The phone then
   reaches the internet, which sees RasBeacon's hotel address.
3. With the VPN on, the internet sees the phone come from the tunnel. When
   the VPN server stops, the kill switch keeps the phone off the internet.
4. A guest phone reaches the internet, but not RasBeacon's settings page or
   the main hotspot's devices.
5. With the allow list on, a phone that isn't listed can't join.
6. With "WPA3 only", a phone joins with WPA3.
7. `rbd` restarts while the guest network runs, and doesn't take the guest
   network's interface for the uplink.
8. The hotspot's interface, taken out of its bridge, is put back by `rbd`'s
   self-check.

Settings are changed through `rbd`'s API socket, as `rbweb` does; the
phones only use the networks (and, in step 1, the setup page's API). Every
wait polls with a time limit instead of sleeping a fixed time.

## Running it

CI runs it in the `system` job (`.github/workflows/ci.yml`). On a Linux
computer with KVM, as root:

```sh
sudo test/system/build-vm.sh /tmp/rbvm   # about 2 minutes
sudo test/system/run-vm.sh /tmp/rbvm     # about 5 minutes with KVM
```

`run-vm.sh` saves the machine's console in `/tmp/rbvm/console.log`: the
test's output and every service's log. Without KVM QEMU still runs it,
about ten times slower.

## Files

- `build-vm.sh` makes the machine: Debian with mmdebstrap, the image's
  overlay (`image/layer/rasbeacon.d/customize.overlay`), RasBeacon's
  programs and the test.
- `run-vm.sh` starts it and reads the result from its console.
- `units/` holds the test's two services: `rasbeacon-test-world.service`
  (before `rbd`) and `rasbeacon-system-test.service` (after `rbweb`; it
  turns the machine off when done).
- `system_test.go` is the test program (`TestWorld`, `TestScenarios`, and
  the helpers `TestHotel` and `TestFetch`, which run inside a namespace).
  Outside the machine every one of them is skipped.
