// VPN page: importing a WireGuard file.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { fakeDevice, renderScreen } from '../render';

const conf = '[Interface]\nPrivateKey = x\nAddress = 10.66.0.2/32\nDNS = 10.66.0.1\n';

describe('importing a WireGuard file', () => {
  it('imports a chosen file under its name', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<VpnScreen />, device);
    await user.upload(screen.getByLabelText(/^WireGuard file/), new File([conf], 'home.conf', { type: 'text/plain' }));
    await user.click(screen.getByRole('button', { name: 'Import' }));
    expect(device.importVpnProfile).toHaveBeenCalledWith({ text: conf, fileName: 'home.conf' });
  });

  it('imports pasted text', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<VpnScreen />, device);
    await user.click(screen.getByLabelText(/^Or paste its contents/));
    await user.paste(conf);
    await user.click(screen.getByRole('button', { name: 'Import' }));
    expect(device.importVpnProfile).toHaveBeenCalledWith(expect.objectContaining({ text: conf }));
  });

  it('shows why a file was refused', async () => {
    const device = fakeDevice(undefined, {
      importVpnProfile: async () => {
        throw new Error('Line 5: RasBeacon never runs commands from a WireGuard file (PostUp).');
      },
    });
    const { user } = renderScreen(<VpnScreen />, device);
    await user.click(screen.getByLabelText(/^Or paste its contents/));
    await user.paste(`${conf}PostUp = rm -rf /\n`);
    await user.click(screen.getByRole('button', { name: 'Import' }));
    expect(await screen.findByText(/never runs commands/)).toBeTruthy();
  });
});
