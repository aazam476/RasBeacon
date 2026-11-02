// Internet page: one Save for every section (lib/pageForm.tsx).
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, openRow, pageSave, renderScreen, section } from '../render';

const bar = () => screen.queryByRole('region', { name: 'Unsaved changes' });

describe('the Internet page’s one Save', () => {
  it('shows nothing until something changes', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    expect(bar()).toBeNull();
    // No section has a Save of its own.
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  });

  it('saves every changed section from the top down, and says the networks are being joined', async () => {
    const device = fakeDevice((router) => {
      router.dnsSettings = { servers: [], withVpn: false };
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Move Starbucks_WiFi_HighSpeed up' }));
    await user.selectOptions(within(section('MAC address')).getByLabelText('Address'), 'random');
    const dns = within(await openRow(user, 'DNS servers'));
    await user.type(dns.getByLabelText(/^DNS servers/), '9.9.9.9');
    await user.click(pageSave());

    expect(device.saveWifiNetworks).toHaveBeenCalledTimes(1);
    expect(device.saveUplinkMac).toHaveBeenCalledWith({ mode: 'random', address: undefined });
    expect(device.saveDnsSettings).toHaveBeenCalledWith('9.9.9.9', false);
    const order = [device.saveWifiNetworks, device.saveUplinkMac, device.saveDnsSettings].map(
      (fn) => fn.mock.invocationCallOrder[0],
    );
    expect(order).toEqual([...order].sort((a, b) => a - b));
    expect(await screen.findByText('Saved. RasBeacon is joining the networks now; see the Status page.')).toBeTruthy();
    // Dismissed, it goes away.
    await user.click(within(bar()!).getByRole('button', { name: 'Dismiss' }));
    expect(bar()).toBeNull();
  });

  it('stops at the first section the router refuses, and points at it', async () => {
    const device = fakeDevice(
      (router) => {
        router.dnsSettings = { servers: [], withVpn: false };
      },
      {
        saveUplinkMac: async () => {
          throw new FieldErrors({ mac: 'Type a MAC address like 3a:1f:22:aa:bb:cc.' });
        },
      },
    );
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.selectOptions(within(section('MAC address')).getByLabelText('Address'), 'cloned');
    const dns = within(await openRow(user, 'DNS servers'));
    await user.type(dns.getByLabelText(/^DNS servers/), '9.9.9.9');
    await user.click(pageSave());

    expect(await screen.findByText('Type a MAC address like 3a:1f:22:aa:bb:cc.')).toBeTruthy();
    expect(within(bar()!).getByText('Check MAC address.')).toBeTruthy();
    expect(device.saveDnsSettings).not.toHaveBeenCalled();
  });

  it('says why a whole section was refused', async () => {
    const device = fakeDevice(undefined, {
      saveWifiNetworks: async () => {
        throw new Error('RasBeacon is busy. Try again.');
      },
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Move Starbucks_WiFi_HighSpeed up' }));
    await user.click(pageSave());
    expect(await within(bar()!).findByText('RasBeacon is busy. Try again.')).toBeTruthy();
  });

  it('throws every change away with Discard', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const mac = within(section('MAC address'));
    await user.selectOptions(mac.getByLabelText('Address'), 'random');
    await user.click(screen.getByRole('button', { name: 'Move Starbucks_WiFi_HighSpeed up' }));
    await user.click(within(bar()!).getByRole('button', { name: 'Discard changes' }));
    expect((mac.getByLabelText('Address') as HTMLSelectElement).value).toBe('');
    expect(bar()).toBeNull();
    expect(device.saveUplinkMac).not.toHaveBeenCalled();
  });
});
