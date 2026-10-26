// The app's two layouts: the phone's tab bar and the computer's sidebar.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import { fakeDevice, renderScreen } from '../render';
import { setViewport } from '../setup';

describe('on a phone', () => {
  it('has a tab bar with four pages and More', () => {
    renderScreen(<Pages />, fakeDevice());
    const tabs = within(screen.getByRole('navigation', { name: 'Main navigation' }));
    expect(tabs.getAllByRole('link').map((link) => link.textContent)).toEqual([
      'Status',
      'Internet',
      'VPN',
      'Clients',
      'More',
    ]);
  });

  it('reaches the other pages through More', async () => {
    const { user } = renderScreen(<Pages />, fakeDevice());
    await user.click(screen.getByRole('link', { name: 'More' }));
    await user.click(within(screen.getByRole('main')).getByRole('link', { name: 'Hotspot' }));
    expect(screen.getByRole('heading', { level: 1, name: 'Hotspot' })).toBeTruthy();
  });
});

describe('on a computer', () => {
  it('lists every page in the sidebar', () => {
    setViewport('desktop');
    renderScreen(<Pages />, fakeDevice());
    const sidebar = within(screen.getByRole('navigation', { name: 'Main navigation' }));
    for (const page of [
      'Status',
      'Internet',
      'VPN',
      'Hotspot',
      'Guests',
      'Clients',
      'Plugins',
      'SSH',
      'HTTPS',
      'System',
    ]) {
      expect(sidebar.getByRole('link', { name: page })).toBeTruthy();
    }
    expect(sidebar.queryByRole('link', { name: 'More' })).toBeNull();
  });

  it('keeps Plugins highlighted on a plugin’s page, and Status only on its own', () => {
    setViewport('desktop');
    renderScreen(<Pages />, fakeDevice(), '/plugins/adguard-home');
    const sidebar = within(screen.getByRole('navigation', { name: 'Main navigation' }));
    expect(sidebar.getByRole('link', { name: 'Plugins' }).getAttribute('aria-current')).toBe('page');
    expect(sidebar.getByRole('link', { name: 'Status' }).getAttribute('aria-current')).toBeNull();
  });

  it('leads back from Browse plugins to Plugins', async () => {
    setViewport('desktop');
    const device = fakeDevice(undefined, { pluginCatalog: async () => ({ entries: [] }) });
    const { user } = renderScreen(<Pages />, device, '/plugins/catalog');
    await user.click(within(screen.getByRole('main')).getByRole('link', { name: 'Plugins' }));
    expect(screen.getByRole('heading', { level: 1, name: 'Plugins' })).toBeTruthy();
  });
});

describe('logging out', () => {
  it.each(['phone', 'desktop'] as const)('logs out on a %s', async (size) => {
    setViewport(size);
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/vpn');
    await user.click(screen.getByRole('button', { name: 'Log out' }));
    expect(device.logOut).toHaveBeenCalledOnce();
  });

  it('says why logging out failed', async () => {
    const device = fakeDevice(undefined, {
      logOut: async () => {
        throw new Error('RasBeacon did not answer.');
      },
    });
    const { user } = renderScreen(<Pages />, device);
    await user.click(screen.getByRole('button', { name: 'Log out' }));
    expect(await screen.findByText('RasBeacon did not answer.')).toBeTruthy();
  });
});
