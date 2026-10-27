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

  it('doesn’t offer WPA3 only when the radio can’t run it, even if it was saved', async () => {
    const device = fakeDevice((router) => {
      router.hotspotSettings = { ...router.hotspotSettings, security: 'wpa3', noWpa3: true };
    });
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    const security = screen.getByLabelText<HTMLSelectElement>('Security');
    expect(Array.from(security.options, (option) => option.value)).toEqual(['wpa2-wpa3', 'wpa2']);
    expect(security.value).toBe('wpa2-wpa3');
    expect(screen.getByText(/This Wi-Fi adapter can't run WPA3 only\./)).toBeTruthy();
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(expect.objectContaining({ security: 'wpa2-wpa3' }));
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
    const japan = { '2.4GHz': [1, 6, 11], '5GHz': [36, 40, 44, 48], '6GHz': [] };

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
        { hotspotChannels: async () => ({ '2.4GHz': [1, 6, 11], '5GHz': [], '6GHz': [] }) },
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

// The owner's report: the settings said channel 36, the radio ran on 40. The page says so.
describe('the channel in use', () => {
  it('says when the radio moved to another channel than the saved one', () => {
    renderScreen(
      <HotspotScreen />,
      fakeDevice((router) => {
        router.status.hotspot = { ...router.status.hotspot, band: '5GHz', channel: 36, actualChannel: 40 };
      }),
    );
    expect(screen.getByText(/Running on channel 40 now/)).toBeTruthy();
  });

  it('says nothing when it runs on the saved one', () => {
    renderScreen(
      <HotspotScreen />,
      fakeDevice((router) => {
        router.status.hotspot = { ...router.status.hotspot, channel: 36, actualChannel: 36 };
      }),
    );
    expect(screen.queryByText(/Running on channel/)).toBeNull();
  });
});

describe('Wi-Fi standard', () => {
  it('offers what a Wi-Fi 5 adapter can run, the best recommended', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    const select = screen.getByLabelText('Wi-Fi standard') as HTMLSelectElement;
    expect([...select.options].map((option) => option.text)).toEqual([
      'Wi-Fi 5 (recommended)',
      'Wi-Fi 4, for very old devices',
    ]);
    await user.selectOptions(select, 'wifi4');
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(expect.objectContaining({ standard: 'wifi4' }));
  });

  it('offers Wi-Fi 6 on an adapter that can run it, and leaves Wi-Fi 5 behind on 2.4 GHz', async () => {
    const device = fakeDevice((router) => {
      router.hotspotSettings = { ...router.hotspotSettings, noWifi6: false, standard: 'wifi5' };
    });
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    const select = () => screen.getByLabelText('Wi-Fi standard') as HTMLSelectElement;
    expect([...select().options].map((option) => option.value)).toEqual(['wifi6', 'wifi5', 'wifi4']);
    expect(select().options[0].text).toBe('Wi-Fi 6 (recommended)');
    await user.selectOptions(screen.getByLabelText('Band'), '2.4GHz');
    expect(select().value).toBe('wifi6');
    expect([...select().options].map((option) => option.value)).toEqual(['wifi6', 'wifi4']);
  });

  it('shows the router’s answer', async () => {
    const device = fakeDevice(undefined, {
      saveHotspotSettings: async () => {
        throw new FieldErrors({ standard: "This Wi-Fi adapter can't run Wi-Fi 6." });
      },
    });
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(await screen.findByText("This Wi-Fi adapter can't run Wi-Fi 6.")).toBeTruthy();
  });

  it('falls back to the best when nothing was chosen', () => {
    const device = fakeDevice((router) => {
      router.hotspotSettings = { ...router.hotspotSettings, standard: undefined };
    });
    renderScreen(<HotspotScreen />, device, '/hotspot');
    expect((screen.getByLabelText('Wi-Fi standard') as HTMLSelectElement).value).toBe('wifi5');
  });
});
