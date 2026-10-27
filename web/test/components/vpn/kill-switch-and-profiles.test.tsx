// VPN page: the kill switch and renaming profiles.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { fakeDevice, renderScreen } from '../render';

describe('kill switch', () => {
  // A new device has the kill switch on, but no profile for it to keep traffic in: it shows off,
  // and doesn't claim to be blocking anything.
  it('is grayed out and off until a WireGuard profile is imported', () => {
    renderScreen(
      <VpnScreen />,
      fakeDevice((router) => {
        router.vpnSettings = {
          ...router.vpnSettings,
          enabled: false,
          killSwitch: true,
          activeProfile: '',
          profiles: [],
        };
        router.status.vpn.killSwitch = true;
      }),
    );
    const killSwitch = screen.getByRole('switch', { name: /Kill switch/ });
    expect(killSwitch).toHaveProperty('disabled', true);
    expect(killSwitch.getAttribute('aria-checked')).toBe('false');
    expect(screen.getByText('Import a WireGuard profile first.')).toBeTruthy();
    expect(screen.queryByText(/Devices only use the internet through the VPN/)).toBeNull();
  });

  it('turns on and off with a profile, and says what each position does', async () => {
    const device = fakeDevice((router) => {
      router.vpnSettings.killSwitch = false;
      router.status.vpn.killSwitch = false;
    });
    const { user } = renderScreen(<VpnScreen />, device);
    expect(screen.getByText('If the VPN goes down, devices use the internet directly.')).toBeTruthy();
    await user.click(screen.getByRole('switch', { name: /Kill switch/ }));
    expect(device.setKillSwitch).toHaveBeenCalledWith(true);
  });

  it('warns that devices have no internet while it is on and the VPN is off', () => {
    renderScreen(
      <VpnScreen />,
      fakeDevice((router) => {
        router.vpnSettings.enabled = false;
        router.vpnSettings.killSwitch = true;
        router.status.vpn = { ...router.status.vpn, enabled: false, state: 'blocked' };
      }),
    );
    expect(screen.getAllByText(/kill switch is on and the VPN is off/).length).toBeGreaterThan(0);
  });
});

describe('profiles', () => {
  it('renames a profile with the pencil next to it', async () => {
    const device = fakeDevice();
    const name = device.vpnSettings.data!.profiles[0].name;
    const { user } = renderScreen(<VpnScreen />, device);
    await user.click(screen.getByRole('button', { name: `Rename ${name}` }));
    const sheet = screen.getByRole('dialog');
    const field = within(sheet).getByRole('textbox', { name: 'Name' });
    await user.clear(field);
    expect(within(sheet).getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
    await user.type(field, 'Home');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.renameVpnProfile).toHaveBeenCalledWith(name, 'Home');
  });
});

describe('use the VPN', () => {
  // The tunnel can't start without the internet, so the switch says why instead of trying (rbd
  // refuses it too). Turning the VPN off always works.
  it('can’t be turned on without internet, and says why', () => {
    renderScreen(
      <VpnScreen />,
      fakeDevice((router) => {
        router.status.uplink.connected = false;
        router.status.vpn = { ...router.status.vpn, enabled: false, state: 'off' };
      }),
    );
    expect(screen.getByRole('switch', { name: /Use the VPN/ })).toHaveProperty('disabled', true);
    expect(screen.getByText('Needs an internet connection')).toBeTruthy();
  });

  it('can still be turned off without internet', async () => {
    const device = fakeDevice((router) => {
      router.status.uplink.connected = false;
    });
    const { user } = renderScreen(<VpnScreen />, device);
    await user.click(screen.getByRole('switch', { name: /Use the VPN/ }));
    expect(device.setVpnEnabled).toHaveBeenCalledWith(false);
  });
});
