// Hotspot page: the network settings, security, and showing the password.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { fakeDevice, renderScreen } from '../render';

describe('hotspot network', () => {
  it('offers WPA3 only, and warns that older devices can’t join', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HotspotScreen />, device);
    await user.selectOptions(screen.getByLabelText('Security'), 'wpa3');
    expect(screen.getByText(/devices older than about 2019 can't join/)).toBeTruthy();

    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(
      expect.objectContaining({ security: 'wpa3', password: '' }),
    );
  });

  it('shows the password and QR code without asking for the admin password', async () => {
    const device = fakeDevice(undefined, {
      revealHotspotPassword: async () => 'kq7m-2xvd-h8pt-rn4w',
      wifiQr: async () => ({ ssid: 'RasBeacon-7162', svg: '<svg></svg>' }),
    });
    const { user } = renderScreen(<HotspotScreen />, device);
    expect(screen.queryByLabelText(/Admin password/)).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Show password and QR code' }));
    expect(await screen.findByText('kq7m-2xvd-h8pt-rn4w')).toBeTruthy();
    expect(device.wifiQr).toHaveBeenCalledWith('hotspot');
  });

  it('says when only the metered hint changed (no restart)', async () => {
    const { user } = renderScreen(<HotspotScreen />, fakeDevice());
    await user.click(screen.getByLabelText(/^Metered connection/));
    expect(screen.getByText('Only the metered setting changed, so the hotspot keeps running.')).toBeTruthy();
  });
});

describe('hotspot form', () => {
  it('a new band goes back to the default channel, and the country is upper case', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    await user.selectOptions(screen.getByLabelText('Band'), '2.4GHz');
    expect(screen.getByLabelText('Channel')).toHaveProperty('value', '0');
    await user.clear(screen.getByLabelText(/^Country/));
    await user.type(screen.getByLabelText(/^Country/), 'de');
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(
      expect.objectContaining({ band: '2.4GHz', channel: 0, country: 'DE', password: '' }),
    );
  });

  it('offers only 2.4 GHz when the radio has no 5 GHz band, even if 5 GHz was saved', async () => {
    const device = fakeDevice((router) => {
      router.hotspotSettings = { ...router.hotspotSettings, band: '5GHz', channel: 44, no5GHz: true };
    });
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    const band = screen.getByLabelText<HTMLSelectElement>('Band');
    expect(Array.from(band.options, (option) => option.value)).toEqual(['2.4GHz']);
    expect(band.value).toBe('2.4GHz');
    expect(screen.getByText(/This Wi-Fi adapter can't use 5 GHz\./)).toBeTruthy();
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(expect.objectContaining({ band: '2.4GHz' }));
  });

  it('says when the hotspot isn’t running', () => {
    renderScreen(
      <HotspotScreen />,
      fakeDevice((router) => {
        router.status.hotspot.running = false;
      }),
      '/hotspot',
    );
    expect(screen.getByText('Not running')).toBeTruthy();
  });
});
