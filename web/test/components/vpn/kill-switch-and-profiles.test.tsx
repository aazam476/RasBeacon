// VPN page: the kill switch and renaming profiles.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { fakeDevice, renderScreen } from '../render';

describe('kill switch', () => {
  it('is grayed out until a WireGuard profile is imported', () => {
    renderScreen(
      <VpnScreen />,
      fakeDevice((router) => {
        router.vpnSettings = { ...router.vpnSettings, enabled: false, activeProfile: '', profiles: [] };
      }),
    );
    expect(screen.getByRole('switch', { name: /Kill switch/ })).toHaveProperty('disabled', true);
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
