// Internet page: the list of Wi-Fi networks to join. Changes collect in the list and are saved
// together with the bar at the bottom.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen, section } from '../render';

function renderList(change?: Parameters<typeof fakeDevice>[0], actions?: Parameters<typeof fakeDevice>[1]) {
  const device = fakeDevice(change, actions);
  return { device, ...renderScreen(<InternetScreen />, device, '/internet') };
}

const order = () =>
  within(section('Wi-Fi networks to join'))
    .getAllByRole('listitem')
    .map((row) => row.textContent?.match(/^\d+(\S+?)(Connected|Open|Username|WPA)/)?.[1]);

/** Save on the bar at the bottom (the DNS row has a Save of its own). */
const saveList = () =>
  within(screen.getByRole('region', { name: 'Unsaved changes' })).getByRole('button', { name: 'Save' });

const savedSsids = (device: ReturnType<typeof fakeDevice>) =>
  device.saveWifiNetworks.mock.calls[0][0].map((network) => network.ssid);

describe('saved networks', () => {
  it('moves a network up and saves the new order', async () => {
    const { device, user } = renderList();
    await user.click(screen.getByRole('button', { name: 'Move Starbucks_WiFi_HighSpeed up' }));
    const bar = within(screen.getByRole('region', { name: 'Unsaved changes' }));
    await user.click(bar.getByRole('button', { name: 'Save' }));
    expect(savedSsids(device)).toEqual(['GrandHyatt_Guest_5G', 'Starbucks_WiFi_HighSpeed', 'eduroam', 'Home-Office']);
    expect(await screen.findByText(/Saved. RasBeacon is joining the networks now/)).toBeTruthy();
  });

  it('the first can’t move up and the last can’t move down', () => {
    renderList();
    expect(screen.getByRole('button', { name: 'Move GrandHyatt_Guest_5G up' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Move Home-Office down' })).toHaveProperty('disabled', true);
  });

  it('removes a network, and Discard changes brings it back', async () => {
    const { device, user } = renderList();
    await user.click(screen.getByRole('button', { name: 'Remove eduroam' }));
    expect(screen.queryByRole('button', { name: 'Remove eduroam' })).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(screen.getByRole('button', { name: 'Remove eduroam' })).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull();
    expect(device.saveWifiNetworks).not.toHaveBeenCalled();
  });

  it('a new password is saved with the list', async () => {
    const { device, user } = renderList();
    await user.click(screen.getByRole('button', { name: 'Change password for Home-Office' }));
    const sheet = within(screen.getByRole('dialog', { name: 'Password' }));
    await user.type(sheet.getByLabelText(/New password/), 'new-office-pass');
    await user.click(sheet.getByRole('button', { name: 'Done' }));
    expect(screen.getByText(/New password/)).toBeTruthy();
    await user.click(saveList());
    const saved = device.saveWifiNetworks.mock.calls[0][0].find((network) => network.ssid === 'Home-Office');
    expect(saved?.password).toBe('new-office-pass');
  });

  it('turns on a random MAC address for one network', async () => {
    const { device, user } = renderList();
    const row = screen.getByRole('button', { name: 'Remove Home-Office' }).closest('li')!;
    await user.click(within(row).getByRole('checkbox', { name: 'Random MAC address' }));
    await user.click(saveList());
    const saved = device.saveWifiNetworks.mock.calls[0][0];
    expect(saved.map((network) => network.randomMac ?? false)).toEqual([false, false, false, true]);
  });

  it('puts the router’s error under the network it is about', async () => {
    const { FieldErrors } = await import('@/data/errors');
    const { user } = renderList(undefined, {
      saveWifiNetworks: async () => {
        throw new FieldErrors({ 'networks.1': 'The CA certificate is not valid.' });
      },
    });
    await user.click(screen.getByRole('button', { name: 'Remove Home-Office' }));
    await user.click(saveList());
    const row = screen.getByRole('button', { name: 'Remove eduroam' }).closest('li')!;
    expect(await within(row).findByText('The CA certificate is not valid.')).toBeTruthy();
  });

  it('shows how an Enterprise login checks the server', () => {
    renderList((router) => {
      const enterprise = router.uplinkSettings.networks[1].enterprise!;
      enterprise.serverName = '';
      enterprise.caCertSummary = '';
      enterprise.serverCertHash = 'ab12cd34ef56ab78' + '0'.repeat(48);
    });
    expect(screen.getByText('Trusted since the first connection (AB:12:CD:34:EF:56:AB:78…)')).toBeTruthy();
  });

  it('warns when the login server is not checked', () => {
    renderList((router) => {
      router.uplinkSettings.networks[1].enterprise!.allowNoCertificateCheck = true;
    });
    expect(screen.getByText('Login server is NOT checked.')).toBeTruthy();
    expect(screen.queryByText('radius.example.edu')).toBeNull();
  });

  it('says when there are none, and when no radio is free', () => {
    renderList((router) => {
      router.uplinkSettings.networks = [];
      router.uplinkSettings.wifiUplinkAvailable = false;
    });
    expect(screen.getByText('No networks saved yet. Add one below.')).toBeTruthy();
    expect(screen.getAllByRole('alert').some((alert) => alert.textContent?.includes('No Wi-Fi radio is free'))).toBe(
      true,
    );
  });

  it('marks the network in use', () => {
    renderList();
    expect(order()[0]).toBe('GrandHyatt_Guest_5G');
    const row = screen.getByRole('button', { name: 'Remove GrandHyatt_Guest_5G' }).closest('li')!;
    expect(row.textContent).toContain('Connected');
  });
});
