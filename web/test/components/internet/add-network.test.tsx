// Internet page: adding a network by hand.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { SavedNetwork } from '@/data/types';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen } from '../render';

const added: SavedNetwork = {
  id: '',
  ssid: 'Campus Wi-Fi',
  security: 'enterprise',
  hidden: false,
  hasPassword: true,
  randomMac: false,
} as SavedNetwork;

async function openAddManually() {
  const device = fakeDevice(undefined, { prepareNetwork: async () => added });
  const view = renderScreen(<InternetScreen />, device, '/internet');
  await view.user.click(screen.getByRole('button', { name: 'Add manually' }));
  return { ...view, device, sheet: within(screen.getByRole('dialog')) };
}

describe('adding a network', () => {
  it('shows example placeholders, not a university’s', async () => {
    const { user, sheet } = await openAddManually();
    expect(sheet.getByLabelText('Network name')).toHaveProperty('placeholder', 'SSID');
    await user.selectOptions(sheet.getByLabelText('Security'), 'enterprise');
    expect(sheet.getByLabelText('Username', { exact: true })).toHaveProperty('placeholder', 'user@example.com');
    expect(sheet.queryByText(/eduroam|Rutgers|NetID/)).toBeNull();
  });

  it('offers WPA-Enterprise without “e.g. eduroam”', async () => {
    const { sheet } = await openAddManually();
    const option = sheet.getByRole('option', { name: /Enterprise/ });
    expect(option.textContent).toBe('Username and password (WPA2/WPA3-Enterprise)');
  });

  it('adds a WPA-Enterprise network without a CA certificate or server name', async () => {
    const { user, sheet, device } = await openAddManually();
    await user.type(sheet.getByLabelText('Network name'), 'Campus Wi-Fi');
    await user.selectOptions(sheet.getByLabelText('Security'), 'enterprise');
    await user.type(sheet.getByLabelText('Username', { exact: true }), 'me@example.com');
    await user.type(sheet.getByLabelText('Your password'), 'campus-password');
    await user.click(sheet.getByRole('button', { name: 'Add to list' }));
    expect(device.prepareNetwork).toHaveBeenCalledWith(
      expect.objectContaining({
        ssid: 'Campus Wi-Fi',
        security: 'enterprise',
        identity: 'me@example.com',
        caCertFiles: [],
        serverName: '',
        allowNoCertificateCheck: false,
      }),
      expect.any(Array),
    );
  });

  it('keeps the CA certificate and server name under Advanced', async () => {
    const { user, sheet } = await openAddManually();
    await user.selectOptions(sheet.getByLabelText('Security'), 'enterprise');
    const advanced = sheet.getByText('Advanced', { exact: true }).closest('details')!;
    expect(advanced.open).toBe(false);
    expect(within(advanced).getByLabelText(/^CA certificate file/)).toBeTruthy();
    expect(within(advanced).getByLabelText('Login server name')).toBeTruthy();
  });
});
