// Internet page: adding a network by hand.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { SavedNetwork } from '@/data/types';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen, section } from '../render';

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

  it('Enterprise with a certificate asks for your certificate, key and passphrase', async () => {
    const { user, sheet } = await openAddManually();
    await user.selectOptions(sheet.getByLabelText('Security'), 'enterprise');
    await user.selectOptions(sheet.getByLabelText('Login method'), 'tls');
    expect(sheet.getByLabelText('Your certificate file')).toBeTruthy();
    expect(sheet.getByLabelText('Your key file')).toBeTruthy();
    expect(sheet.getByLabelText(/^Key passphrase/)).toBeTruthy();
    expect(sheet.queryByLabelText('Your password')).toBeNull();
  });

  it('warns before not checking the login server', async () => {
    const { user, sheet } = await openAddManually();
    await user.selectOptions(sheet.getByLabelText('Security'), 'enterprise');
    await user.click(sheet.getByText('Advanced', { exact: true }));
    await user.click(sheet.getByLabelText(/Don't check the login server/));
    expect(sheet.getByText("The login server won't be checked")).toBeTruthy();
  });

  it('shows a problem next to its field until the field changes', async () => {
    const { FieldErrors } = await import('@/data/errors');
    const device = fakeDevice(undefined, {
      prepareNetwork: async () => {
        throw new FieldErrors({ ssid: 'This network is already in the list.' });
      },
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Add manually' }));
    const sheet = within(screen.getByRole('dialog'));
    await user.type(sheet.getByLabelText('Network name'), 'eduroam');
    await user.click(sheet.getByRole('button', { name: 'Add to list' }));
    expect(await sheet.findByText('This network is already in the list.')).toBeTruthy();
    await user.type(sheet.getByLabelText('Network name'), '2');
    expect(sheet.queryByText('This network is already in the list.')).toBeNull();
  });
});

describe('nearby networks', () => {
  it('Look for nearby networks asks the router', async () => {
    const device = fakeDevice((router) => {
      router.nearbyNetworks = [];
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Look for nearby networks' }));
    expect(device.scanNearbyNetworks).toHaveBeenCalledOnce();
    expect(screen.getByText('No networks found.')).toBeTruthy();
  });

  it('marks networks in use, saved, or not supported, and only offers the others', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    const nearby = within(section('Add a network'));
    expect(nearby.queryByRole('button', { name: 'GrandHyatt_Guest_5G' })).toBeNull();
    expect(nearby.queryByRole('button', { name: 'Starbucks_WiFi_HighSpeed' })).toBeNull();
    expect(nearby.queryByRole('button', { name: 'Lobby_Display_Bridge' })).toBeNull();
    expect(nearby.getByText(/Not supported/)).toBeTruthy();
    expect(nearby.getByRole('button', { name: 'MobileMiFi_Roaming_7A' })).toBeTruthy();
  });

  it('a found network only needs its password', async () => {
    const device = fakeDevice(undefined, { prepareNetwork: async () => added });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'MobileMiFi_Roaming_7A' }));
    const sheet = within(screen.getByRole('dialog'));
    expect(sheet.queryByLabelText('Network name')).toBeNull();
    await user.type(sheet.getByLabelText('Network password'), 'mifi-pass-1');
    await user.click(sheet.getByRole('button', { name: 'Add to list' }));
    expect(device.prepareNetwork).toHaveBeenCalledWith(
      expect.objectContaining({ ssid: 'MobileMiFi_Roaming_7A', security: 'personal', password: 'mifi-pass-1' }),
      expect.any(Array),
    );
    // Added to the list, to be saved with it.
    expect(screen.getByRole('region', { name: 'Unsaved changes' })).toBeTruthy();
  });

  it('a found Enterprise network opens with its login settings', async () => {
    const { user } = renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    await user.click(screen.getByRole('button', { name: 'GrandHyatt_Staff_Secure' }));
    expect(within(screen.getByRole('dialog')).getByText('Enterprise login')).toBeTruthy();
  });

  it('shows why the scan failed', async () => {
    const device = fakeDevice(undefined, {
      scanNearbyNetworks: async () => {
        throw new Error('The radio is busy.');
      },
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Look for nearby networks' }));
    expect(await screen.findByText('The radio is busy.')).toBeTruthy();
  });
});
