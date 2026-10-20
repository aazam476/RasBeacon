// The router's states in words (Status, Internet and VPN pages).
import { describe, expect, it } from 'vitest';
import { createMockRouter } from '@/data/demo/mockData';
import type { Security, VPNStatus } from '@/data/types';
import { describeIpv6, describeSecurity, describeUplink, uplinkBadge, vpnStateView } from '@/screens/common/describe';

const sample = createMockRouter().status;

describe('hotspot security', () => {
  it.each<[Security, string]>([
    ['wpa2-wpa3', 'WPA2/WPA3'],
    ['wpa3', 'WPA3 only'],
    ['wpa2', 'WPA2 only'],
  ])('%s reads "%s"', (security, words) => {
    expect(describeSecurity(security)).toBe(words);
  });
});

describe('the internet connection', () => {
  it('names the Wi-Fi network, or says how it is connected', () => {
    const wifi = {
      ...sample.uplink,
      connected: true,
      kind: 'wifi' as const,
      wifi: { ...sample.uplink.wifi, ssid: 'Corner Café' },
    };
    expect(describeUplink(wifi)).toBe('Connected to Wi-Fi "Corner Café"');
    expect(describeUplink({ ...wifi, kind: 'ethernet' })).toBe('Connected by Ethernet');
    expect(describeUplink({ ...wifi, connected: false })).toBe('Not connected');
    expect(uplinkBadge(wifi)).toEqual({ label: 'Online', tone: 'green' });
  });

  it('names the public IPv6 network the VPN routes', () => {
    expect(describeIpv6(sample.uplink, { ...sample.vpn, routedIpv6Prefix: '2001:db8::/64' })).toBe(
      'Public addresses through the VPN (2001:db8::/64)',
    );
  });
});

describe('the VPN', () => {
  const vpn = (state: VPNStatus['state'], enabled: boolean, killSwitch: boolean): VPNStatus => ({
    ...sample.vpn,
    state,
    enabled,
    killSwitch,
  });

  it.each([
    ['up', vpn('up', true, true), 'Protected', 'green'],
    ['down with the kill switch', vpn('down', true, true), 'No internet', 'orange'],
    ['connecting without it', vpn('connecting', true, false), 'Connecting…', 'gray'],
    ['direct', vpn('direct', true, false), 'Not protected', 'red'],
    ['off', vpn('off', false, false), 'VPN off', 'gray'],
    ['blocked by the kill switch while off', vpn('blocked', false, true), 'No internet', 'red'],
  ] as const)('%s: "%s"', (_, status, title, tone) => {
    expect(vpnStateView(status)).toMatchObject({ title, tone });
  });

  it('explains why devices have no internet while blocked, as an alert', () => {
    expect(vpnStateView(vpn('blocked', false, true))).toMatchObject({
      summary: 'Devices have no internet: the kill switch is on and the VPN is off.',
      alert: true,
    });
  });
});
