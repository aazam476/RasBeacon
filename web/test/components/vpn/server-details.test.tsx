// VPN page: the profile's server details, RasBeacon's public key, choosing and removing profiles.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { fakeDevice, openRow, renderScreen } from '../render';

describe('server details', () => {
  it('shows the imported settings, and saves a change with the keys left empty', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<VpnScreen />, device, '/vpn');
    const row = within(await openRow(user, 'Server details (advanced)'));
    const server = row.getByLabelText(/^Server \(host:port\)/);
    expect(server).toHaveProperty('value', '198.51.100.44:51820');
    await user.clear(server);
    await user.type(server, 'vpn.example.com:51820');
    await user.click(row.getByRole('button', { name: 'Save' }));
    expect(device.saveVpnDetails).toHaveBeenCalledWith(
      'hotel-tunnel-01.conf',
      expect.objectContaining({ endpoint: 'vpn.example.com:51820', privateKey: '', presharedKey: '' }),
    );
    expect(await row.findByText('Saved.')).toBeTruthy();
  });

  it('offers all traffic as the allowed IPs when the file had none', async () => {
    const device = fakeDevice((router) => {
      router.vpnSettings.profiles[0].allowedIps = [];
    });
    const { user } = renderScreen(<VpnScreen />, device, '/vpn');
    const row = within(await openRow(user, 'Server details (advanced)'));
    expect(row.getByLabelText(/^Allowed IPs/)).toHaveProperty('value', '0.0.0.0/0, ::/0');
  });
});

describe('public key', () => {
  it('makes a new key pair after warning', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<VpnScreen />, device, '/vpn');
    await user.click(screen.getByRole('button', { name: 'Generate new key' }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Generate a new key?' })).getByRole('button', { name: 'Generate' }),
    );
    expect(device.generateVpnKey).toHaveBeenCalledWith('hotel-tunnel-01.conf');
    expect(await screen.findByText(/New key pair made/)).toBeTruthy();
  });
});

describe('profiles', () => {
  it('uses another profile when it is chosen', async () => {
    const device = fakeDevice();
    const other = device.vpnSettings.data!.profiles[1].name;
    const { user } = renderScreen(<VpnScreen />, device, '/vpn');
    await user.click(screen.getByRole('radio', { name: new RegExp(other) }));
    expect(device.setActiveVpnProfile).toHaveBeenCalledWith(other);
  });

  it('removes a profile after asking, and says when it is the one in use', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<VpnScreen />, device, '/vpn');
    await user.click(screen.getByRole('button', { name: 'Remove hotel-tunnel-01.conf' }));
    const alert = within(screen.getByRole('dialog', { name: 'Remove hotel-tunnel-01.conf?' }));
    expect(alert.getByText(/It is the profile in use/)).toBeTruthy();
    await user.click(alert.getByRole('button', { name: 'Remove' }));
    expect(device.removeVpnProfile).toHaveBeenCalledWith('hotel-tunnel-01.conf');
  });
});
