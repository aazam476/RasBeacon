// A plugin's own page: running it, and what it can do.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import { fakeDevice, openRow, renderScreen } from '../render';

describe('a plugin’s page', () => {
  it('stops and starts it with the Running switch', async () => {
    const device = fakeDevice();
    const plugin = device.plugins.data!.plugins.find((p) => p.id === 'adguard-home')!;
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    expect(screen.getByRole('heading', { level: 1, name: plugin.name })).toBeTruthy();
    const running = screen.getByRole('switch', { name: /Running/ });
    await user.click(running);
    expect(device.setPluginRunning).toHaveBeenCalledWith('adguard-home', plugin.state !== 'running');
  });

  it('lists what it may do', () => {
    renderScreen(<Pages />, fakeDevice(), '/plugins/adguard-home');
    const section = screen.getByRole('heading', { name: 'It can' }).closest('section')!;
    expect(within(section).getByText('Use the internet')).toBeTruthy();
  });
});

describe('looking after a plugin', () => {
  const adguard = (device: ReturnType<typeof fakeDevice>) =>
    device.plugins.data!.plugins.find((p) => p.id === 'adguard-home')!;

  it('saves its settings, with the ports as numbers', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    const port = adguard(device).ports[0];
    const field = screen.getByLabelText(new RegExp(`^${port.name}`));
    await user.clear(field);
    await user.type(field, '8053');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.savePluginSettings).toHaveBeenCalledWith(
      'adguard-home',
      expect.objectContaining({ ports: expect.objectContaining({ [port.key]: 8053 }) }),
    );
  });

  it('shows its log when opened', async () => {
    const device = fakeDevice(undefined, { pluginLogs: async () => ['started', 'listening on :53'] });
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    const row = within(await openRow(user, 'Log'));
    expect(await row.findByText(/listening on :53/)).toBeTruthy();
  });

  it('says why the log can’t be read', async () => {
    const device = fakeDevice(undefined, {
      pluginLogs: async () => {
        throw new Error('It is not running.');
      },
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    const row = within(await openRow(user, 'Log'));
    expect(await row.findByText('It is not running.')).toBeTruthy();
  });

  it('updates from the catalog when a newer signed version is there', async () => {
    const device = fakeDevice((router) => {
      const plugin = router.plugins.plugins.find((p) => p.id === 'adguard-home')!;
      plugin.updateVersion = '9.9.9';
      plugin.canUpdate = true;
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    await user.click(screen.getByRole('button', { name: 'Update to 9.9.9' }));
    expect(device.previewPluginUpdate).toHaveBeenCalledWith('adguard-home');
  });

  it('removes it, keeping its data when asked, then goes back to Plugins', async () => {
    const device = fakeDevice();
    const name = adguard(device).name;
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    await user.click(screen.getByRole('button', { name: `Remove ${name}` }));
    const alert = within(screen.getByRole('dialog', { name: `Remove ${name}?` }));
    await user.click(alert.getByRole('checkbox', { name: /Keep its data/ }));
    await user.click(alert.getByRole('button', { name: 'Remove' }));
    expect(device.removePlugin).toHaveBeenCalledWith('adguard-home', true);
    expect(await screen.findByRole('heading', { level: 1, name: 'Plugins' })).toBeTruthy();
  });

  it('an unknown plugin goes back to Plugins', async () => {
    renderScreen(<Pages />, fakeDevice(), '/plugins/nothing-here');
    expect(await screen.findByRole('heading', { level: 1, name: 'Plugins' })).toBeTruthy();
  });

  it('a plugin whose files can’t be read offers only its log, updating and removing', () => {
    const device = fakeDevice((router) => {
      router.plugins.plugins.find((p) => p.id === 'adguard-home')!.broken = true;
    });
    renderScreen(<Pages />, device, '/plugins/adguard-home');
    expect(screen.queryByRole('switch', { name: /Running/ })).toBeNull();
    expect(screen.getByText('Log')).toBeTruthy();
    expect(screen.getByRole('button', { name: /^Remove / })).toBeTruthy();
  });
});
