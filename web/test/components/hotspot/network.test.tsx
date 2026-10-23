// Hotspot page: the network settings, security, and showing the password.
import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
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

  // The channels come from the router's regulatory database, for the country typed.
  describe('channels by country', () => {
    const japan = { '2.4GHz': [1, 6, 11], '5GHz': [36, 40, 44, 48] };

    it('offers only the channels the country allows', async () => {
      const device = fakeDevice(undefined, { hotspotChannels: async () => japan });
      const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
      await user.clear(screen.getByLabelText(/^Country/));
      await user.type(screen.getByLabelText(/^Country/), 'jp');
      await user.selectOptions(screen.getByLabelText('Band'), '5GHz');
      expect(device.hotspotChannels).toHaveBeenLastCalledWith('JP');
      const channels = () => Array.from(screen.getByLabelText<HTMLSelectElement>('Channel').options, (o) => o.value);
      await waitFor(() => expect(channels()).toEqual(['0', '36', '40', '44', '48']));
    });

    it('keeps a channel the new country forbids visible, and says so', async () => {
      const device = fakeDevice(
        (router) => {
          router.hotspotSettings = { ...router.hotspotSettings, band: '5GHz', channel: 149, country: 'US' };
        },
        { hotspotChannels: async (country: string) => (country === 'JP' ? japan : { ...japan, '5GHz': [36, 149] }) },
      );
      const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
      await user.clear(screen.getByLabelText(/^Country/));
      await user.type(screen.getByLabelText(/^Country/), 'JP');
      expect(await screen.findByText("This channel isn't allowed in this country.")).toBeTruthy();
      expect(screen.getByRole('option', { name: '149 (not allowed here)' })).toBeTruthy();
      await user.selectOptions(screen.getByLabelText('Channel'), '36');
      expect(screen.queryByText("This channel isn't allowed in this country.")).toBeNull();
    });

    it('says at once when the code isn’t a country', async () => {
      const device = fakeDevice(undefined, {
        hotspotChannels: async () => {
          throw new FieldErrors({ country: 'Use GB for the United Kingdom.' });
        },
      });
      const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
      await user.clear(screen.getByLabelText(/^Country/));
      await user.type(screen.getByLabelText(/^Country/), 'UK');
      expect(await screen.findByText('Use GB for the United Kingdom.')).toBeTruthy();
    });

    it('says when the country has no 5 GHz hotspot, and offers 5 GHz no more', async () => {
      const device = fakeDevice(
        (router) => {
          router.hotspotSettings = { ...router.hotspotSettings, band: '5GHz', channel: 0 };
        },
        { hotspotChannels: async () => ({ '2.4GHz': [1, 6, 11], '5GHz': [] }) },
      );
      const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
      expect(await screen.findByText("This country doesn't allow a hotspot on 5 GHz.")).toBeTruthy();
      await user.selectOptions(screen.getByLabelText('Band'), '2.4GHz');
      const bands = Array.from(screen.getByLabelText<HTMLSelectElement>('Band').options, (o) => o.value);
      expect(bands).toEqual(['2.4GHz']);
    });

    it('offers every channel while the router hasn’t answered, or couldn’t', async () => {
      const device = fakeDevice(undefined, {
        hotspotChannels: async () => {
          throw new Error("RasBeacon didn't answer.");
        },
      });
      renderScreen(<HotspotScreen />, device, '/hotspot');
      await waitFor(() => expect(device.hotspotChannels).toHaveBeenCalled());
      // The sample hotspot is on 5 GHz: Default and all 9 channels, 149 included.
      const channels = Array.from(screen.getByLabelText<HTMLSelectElement>('Channel').options, (o) => o.value);
      expect(channels).toEqual(['0', '36', '40', '44', '48', '149', '153', '157', '161', '165']);
    });
  });
});
