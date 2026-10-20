// Small parts used across the pages: countdowns, the device's provider, the demo build, QR codes
// that can be hidden again, and what the install sheet says about a plugin.
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createMockPlugins, definitionOf, previewOf } from '@/data/demo/plugins';
import { useDevice } from '@/data/device';
import type { PluginPreview } from '@/data/types';
import { useCountdown } from '@/lib/useCountdown';
import { GuestsScreen } from '@/screens/guests/GuestsScreen';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { InstallSheet } from '@/screens/plugins/InstallSheet';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { fakeDevice, renderScreen } from '../render';

afterEach(() => vi.useRealTimers());

describe('countdown', () => {
  function Countdown({ seconds }: { seconds: number }) {
    return <p>{useCountdown(seconds)}</p>;
  }

  it('counts down each second, stops at 0, and starts over from a new value', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const { rerender } = render(<Countdown seconds={2} />);
    expect(screen.getByText('2')).toBeTruthy();
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(screen.getByText('1')).toBeTruthy();
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(screen.getByText('0')).toBeTruthy();
    rerender(<Countdown seconds={30} />);
    expect(screen.getByText('30')).toBeTruthy();
  });
});

describe('the device', () => {
  it('says plainly when a screen is used without a device provider', () => {
    function Screen() {
      useDevice();
      return null;
    }
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(() => render(<Screen />)).toThrow(/useDevice\(\) must be used inside a device provider/);
  });
});

describe('QR codes', () => {
  it('the hotspot’s can be hidden again', async () => {
    const device = fakeDevice(undefined, {
      revealHotspotPassword: async () => 'kq7m-2xvd-h8pt-rn4w',
      wifiQr: async () => ({ ssid: 'RasBeacon-7162', svg: '<svg></svg>' }),
    });
    const { user } = renderScreen(<HotspotScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Show password and QR code' }));
    await user.click(await screen.findByRole('button', { name: 'Hide' }));
    expect(screen.queryByText('kq7m-2xvd-h8pt-rn4w')).toBeNull();
  });

  it('the guest network’s too', async () => {
    const device = fakeDevice(undefined, { wifiQr: async () => ({ ssid: 'RasBeacon-Guests', svg: '<svg></svg>' }) });
    const { user } = renderScreen(<GuestsScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Show QR code' }));
    expect(device.wifiQr).toHaveBeenCalledWith('guest');
    await user.click(await screen.findByRole('button', { name: 'Hide QR code' }));
    expect(screen.getByRole('button', { name: 'Show QR code' })).toBeTruthy();
  });
});

describe('install sheet', () => {
  /** The demo's Trip Notes plugin, changed by `change`. */
  function renderInstall(change: Partial<PluginPreview>) {
    const preview = { ...previewOf(definitionOf('trip-notes')!, createMockPlugins(), 'staging-1'), ...change };
    renderScreen(<InstallSheet preview={preview} onDone={() => undefined} />, fakeDevice());
    return within(screen.getByRole('dialog'));
  }

  it('shows the file’s warnings, and says when it may do nothing more', () => {
    const sheet = renderInstall({ warnings: ['It is not signed.'], permissions: [] });
    expect(sheet.getByText('It is not signed.')).toBeTruthy();
    expect(sheet.getByText('Nothing more')).toBeTruthy();
  });

  it.each([
    [1, 'Up to one core'],
    [0.5, 'Up to 50% of a core'],
    [2, 'Up to 2 cores'],
  ])('%s processor reads "%s"', (cpus, text) => {
    expect(renderInstall({ cpus }).getByText(text)).toBeTruthy();
  });

  it.each([
    [0, 'None (in the file)', null],
    [-1, 'Size unknown', /its size is unknown/],
    // Sizes keep their unit on the same line (a no-break space).
    [52_000_000, /^52\.0\sMB$/, /downloads 52\.0\sMB through your metered connection/],
  ])('a download of %s reads %s, with a warning when metered', (downloadSize, text, warning) => {
    const sheet = renderInstall({ downloadSize, metered: true });
    expect(sheet.getByText(text)).toBeTruthy();
    if (warning) expect(sheet.getByText(warning)).toBeTruthy();
    else expect(sheet.queryByText('Metered connection')).toBeNull();
  });
});

describe('Plugins page', () => {
  it('says what plugins are when none is installed', () => {
    const device = fakeDevice((router) => {
      router.plugins.plugins = [];
    });
    renderScreen(<PluginsScreen />, device, '/plugins');
    expect(screen.getByRole('link', { name: /Browse plugins/ })).toBeTruthy();
  });

  it('says why a plugin file was refused', async () => {
    const device = fakeDevice(undefined, {
      previewPluginFile: async () => {
        throw new Error('The file is not a RasBeacon plugin.');
      },
    });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'x.rba'));
    expect(await screen.findByText('The file is not a RasBeacon plugin.')).toBeTruthy();
  });
});
