// Hotspot page: 6 GHz (Wi-Fi 6E), on an adapter that can run it, where the country allows it.
import { screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { fakeDevice, renderScreen } from '../render';

const germany = { '2.4GHz': [1, 6, 11], '5GHz': [36, 40, 44, 48], '6GHz': [5, 21, 37, 53, 69, 85] };
const unitedStates = { ...germany, '6GHz': [] };

/** A Wi-Fi 6E adapter in Germany. */
function sixGHzAdapter(router: MockRouter) {
  router.hotspotSettings = {
    ...router.hotspotSettings,
    country: 'DE',
    has6GHz: true,
    noWifi6: false,
    standard: 'wifi6',
  };
}

const options = (label: string) =>
  Array.from(screen.getByLabelText<HTMLSelectElement>(label).options, (option) => option.value);

describe('6 GHz hotspot', () => {
  it('is offered, and choosing it makes the hotspot WPA3 only and Wi-Fi 6E', async () => {
    const device = fakeDevice(sixGHzAdapter, { hotspotChannels: async () => germany });
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    await waitFor(() => expect(options('Band')).toEqual(['2.4GHz', '5GHz', '6GHz']));
    await user.selectOptions(screen.getByLabelText('Band'), '6GHz');
    expect(options('Security')).toEqual(['wpa3']);
    expect(options('Wi-Fi standard')).toEqual(['wifi6']);
    expect(within(screen.getByLabelText('Wi-Fi standard')).getByRole('option').textContent).toBe('Wi-Fi 6E');
    expect(options('Channel')).toEqual(['0', '5', '21', '37', '53', '69', '85']);
    expect(screen.getByText('6 GHz networks are WPA3 only.')).toBeTruthy();
    expect(screen.getByText(/Only Wi-Fi 6E devices \(from about 2021\) see a 6 GHz network/)).toBeTruthy();
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(
      expect.objectContaining({ band: '6GHz', security: 'wpa3', standard: 'wifi6', channel: 0 }),
    );

    // Back to 5 GHz: the recommended security again.
    await user.selectOptions(screen.getByLabelText('Band'), '5GHz');
    expect((screen.getByLabelText('Security') as HTMLSelectElement).value).toBe('wpa2-wpa3');
  });

  it('isn’t offered where the country doesn’t allow it, and says so', async () => {
    const device = fakeDevice(sixGHzAdapter, { hotspotChannels: async () => unitedStates });
    renderScreen(<HotspotScreen />, device, '/hotspot');
    await waitFor(() => expect(options('Band')).toEqual(['2.4GHz', '5GHz']));
    expect(screen.getByText(/This country doesn't allow a 6 GHz hotspot, so 6 GHz isn't offered/)).toBeTruthy();
  });

  it('says when the saved 6 GHz isn’t allowed in a new country', async () => {
    const device = fakeDevice(
      (router) => {
        sixGHzAdapter(router);
        Object.assign(router.hotspotSettings, { band: '6GHz', security: 'wpa3', channel: 0 });
      },
      { hotspotChannels: async (country: string) => (country === 'US' ? unitedStates : germany) },
    );
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    await user.clear(screen.getByLabelText(/^Country/));
    await user.type(screen.getByLabelText(/^Country/), 'US');
    expect(await screen.findByText("This country doesn't allow a hotspot on 6 GHz.")).toBeTruthy();
  });

  it('starts on 5 GHz when the adapter can’t run 6 GHz any more', () => {
    const device = fakeDevice((router) => {
      Object.assign(router.hotspotSettings, { band: '6GHz', security: 'wpa3', has6GHz: false });
    });
    renderScreen(<HotspotScreen />, device, '/hotspot');
    expect((screen.getByLabelText('Band') as HTMLSelectElement).value).toBe('5GHz');
  });
});
