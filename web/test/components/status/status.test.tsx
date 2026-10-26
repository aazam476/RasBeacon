// The Status page: whether devices are protected, what needs attention, and a group for each
// part of RasBeacon.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import { StatusScreen } from '@/screens/status/StatusScreen';
import { fakeDevice, renderScreen, section } from '../render';

function renderStatus(change?: (status: MockRouter['status']) => void, actions?: Parameters<typeof fakeDevice>[1]) {
  const device = fakeDevice((router) => change?.(router.status), actions);
  return { device, ...renderScreen(<StatusScreen />, device) };
}

describe('the answer at the top', () => {
  it('says devices are protected while the VPN is up', () => {
    renderStatus();
    expect(screen.getByRole('heading', { level: 2, name: 'Protected' })).toBeTruthy();
  });

  it('changes softly when the state changes', () => {
    const { device, rerender } = renderStatus();
    const animations: unknown[] = [];
    const symbol = screen.getByRole('heading', { name: 'Protected' }).closest('section')!.querySelector('span')!;
    symbol.animate = (keyframes: unknown) => {
      animations.push(keyframes);
      return {} as Animation;
    };
    Object.assign(device, {
      status: { data: { ...device.status.data!, vpn: { ...device.status.data!.vpn, enabled: false } }, error: null },
    });
    rerender(<StatusScreen />);
    expect(screen.getByRole('heading', { level: 2, name: 'VPN off' })).toBeTruthy();
    expect(animations).toHaveLength(1);
  });
});

describe('what needs attention', () => {
  it('lists RasBeacon’s warnings together', () => {
    renderStatus((status) => {
      status.warnings = ['The SD card is almost full.', 'The power supply is too weak.'];
    });
    const warnings = within(screen.getByRole('list', { name: 'Warnings' }));
    expect(warnings.getAllByRole('alert').map((w) => w.textContent)).toEqual([
      'The SD card is almost full.',
      'The power supply is too weak.',
    ]);
  });

  it('says when the network refused the login, with the way to fix it', () => {
    renderStatus((status) => {
      status.uplink.connected = false;
      status.uplink.wifi.state = 'login-failed';
    });
    expect(screen.getByText(/The network rejected the login/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open Internet settings' }).getAttribute('href')).toBe('/internet');
  });
});

describe('internet', () => {
  it('shows the Wi-Fi network in use and the last speed test', () => {
    renderStatus();
    const internet = within(section('Internet'));
    expect(internet.getByText('Online')).toBeTruthy();
    expect(internet.getByText('Connected to Wi-Fi "GrandHyatt_Guest_5G"')).toBeTruthy();
    expect(internet.getByText(/ ago, Cloudflare$/)).toBeTruthy();
  });

  it.each([
    {
      when: 'the cable carries it',
      change: (s: MockRouter['status']) => {
        s.uplink.kind = 'ethernet';
        s.uplink.ethernet.cablePlugged = true;
      },
      says: 'Connected by Ethernet',
      badge: 'Online',
    },
    {
      when: 'it is joining a network',
      change: (s: MockRouter['status']) => {
        s.uplink.connected = false;
        s.uplink.wifi.state = 'connecting';
      },
      says: 'Not connected',
      badge: 'Connecting',
    },
    {
      when: 'no radio is free',
      change: (s: MockRouter['status']) => {
        s.uplink.connected = false;
        s.uplink.wifi.available = false;
      },
      says: 'No radio free to join networks',
      badge: 'Not connected',
    },
    {
      when: 'no saved network is in range',
      change: (s: MockRouter['status']) => {
        s.uplink.connected = false;
        s.uplink.wifi.state = 'searching';
      },
      says: 'Looking for your saved networks',
      badge: 'Not connected',
    },
  ])('says how it is connected when $when', ({ change, says, badge }) => {
    renderStatus(change);
    const internet = within(section('Internet'));
    expect(internet.getByText(says)).toBeTruthy();
    expect(internet.getAllByText(badge).length).toBeGreaterThan(0);
  });
});

describe('VPN', () => {
  it('turns the VPN and the kill switch on and off from here', async () => {
    const { device, user } = renderStatus();
    const vpn = within(section('VPN'));
    await user.click(vpn.getByRole('switch', { name: /Use the VPN/ }));
    await user.click(vpn.getByRole('switch', { name: /Kill switch/ }));
    expect(device.setVpnEnabled).toHaveBeenCalledWith(false);
    expect(device.setKillSwitch).toHaveBeenCalledWith(false);
  });

  // Saving a switch sends the other's saved value too, so both wait for the VPN settings.
  it('the kill switch waits for the VPN settings to load', () => {
    const device = fakeDevice();
    device.vpnSettings = { data: undefined, error: null };
    renderScreen(<StatusScreen />, device);
    const vpn = within(section('VPN'));
    expect(vpn.getByRole('switch', { name: /Kill switch/ })).toHaveProperty('disabled', true);
    expect(vpn.getByRole('switch', { name: /Use the VPN/ })).toHaveProperty('disabled', true);
  });

  // A new device has the kill switch on, but it does nothing until a profile exists. Status
  // shows it the way the VPN page does: off and grayed out.
  it('the kill switch shows off and can’t be pressed without a profile', () => {
    const device = fakeDevice((router) => {
      router.status.vpn.killSwitch = true;
      router.vpnSettings.profiles = [];
      router.vpnSettings.activeProfile = '';
    });
    renderScreen(<StatusScreen />, device);
    const killSwitch = within(section('VPN')).getByRole('switch', { name: /Kill switch/ });
    expect(killSwitch).toHaveProperty('disabled', true);
    expect(killSwitch.getAttribute('aria-checked')).toBe('false');
  });

  it('“Use the VPN” can’t be turned on without internet, and says why', () => {
    renderStatus((status) => {
      status.uplink.connected = false;
      status.vpn = { ...status.vpn, enabled: false, state: 'off' };
    });
    const vpn = within(section('VPN'));
    expect(vpn.getByRole('switch', { name: /Use the VPN/ })).toHaveProperty('disabled', true);
    expect(vpn.getByText('Needs an internet connection')).toBeTruthy();
  });
});

describe('devices and networks', () => {
  it('lists a few devices and links to all of them', () => {
    renderStatus((status) => {
      status.clients = Array.from({ length: 12 }, (_, i) => ({
        ...status.clients[0],
        mac: `3a:1f:22:aa:bb:${String(i).padStart(2, '0')}`,
        name: `Device ${i + 1}`,
      }));
    });
    const devices = within(section('Connected devices (12)'));
    expect(devices.getByRole('link', { name: /All 12 devices/ })).toBeTruthy();
    expect(devices.queryByText('Device 12')).toBeNull();
  });

  it('says when no device is connected', () => {
    renderStatus((status) => {
      status.clients = [];
    });
    expect(within(section('Connected devices')).getByText('No devices connected.')).toBeTruthy();
  });

  it('offers to set up a guest network when there is none', () => {
    renderStatus((status) => {
      status.guest = { ...status.guest, enabled: false, clients: [] };
    });
    expect(screen.getByRole('link', { name: /Set up a guest network/ })).toBeTruthy();
  });
});
