// Plugins: choosing the size of a plugin's data on RasBeacon's own storage, for a plugin whose
// manifest allows it (the demo's Samba), and seeing it in System → Storage.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import type { MockRouter } from '@/data/demo/mockData';
import { createMockPlugins, definitionOf, previewOf } from '@/data/demo/plugins';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen, section } from '../render';

/** The sample router with Samba's files on RasBeacon's internal storage (no USB drive). */
function sambaOnInternalStorage(router: MockRouter) {
  router.plugins.plugins.find((p) => p.id === 'samba')!.drive = undefined;
}

/** The install sheet's preview for Samba, as a new install (Samba isn't installed). */
function sambaPreview() {
  const state = createMockPlugins();
  state.plugins = state.plugins.filter((p) => p.id !== 'samba');
  return previewOf(definitionOf('samba')!, state, 'staging-9');
}

describe('the size of a plugin’s data', () => {
  it('is chosen when installing, in GB, with the sizes it may have', async () => {
    const device = fakeDevice(undefined, { previewPluginFile: async () => sambaPreview() });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'samba.rba'));
    const sheet = within(await screen.findByRole('dialog', { name: 'Install Samba' }));
    const size = sheet.getByRole('textbox', { name: /^Size/ }) as HTMLInputElement;
    expect(size.value).toBe('2');
    expect(sheet.getByText('From 0.3 to 6 GB.')).toBeTruthy();
    await user.clear(size);
    await user.type(size, '3.5');
    // Without a drive, the share is on RasBeacon's own storage.
    await user.selectOptions(sheet.getByLabelText('Drive'), 'RasBeacon internal storage');
    await user.type(sheet.getByLabelText('Password'), 'harbor-sunrise');
    await user.click(sheet.getByRole('button', { name: 'Install' }));
    expect(device.installPlugin).toHaveBeenCalledWith(
      'staging-9',
      expect.objectContaining({ storageMb: 3584, drive: '' }),
      false,
    );
  });

  it('isn’t asked for by an update, which keeps it', async () => {
    const state = createMockPlugins();
    const update = previewOf(definitionOf('samba')!, state, 'staging-10');
    const device = fakeDevice(undefined, { previewPluginFile: async () => update });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'samba.rba'));
    const sheet = within(await screen.findByRole('dialog', { name: 'Update Samba' }));
    // The storage size (not Samba's size limit, which is about its drive).
    expect(sheet.queryByRole('textbox', { name: /^Size(?! limit)/ })).toBeNull();
    await user.click(sheet.getByRole('button', { name: 'Update' }));
    expect(device.installPlugin).toHaveBeenCalledWith(
      'staging-10',
      expect.not.objectContaining({ storageMb: expect.anything() }),
      false,
    );
  });

  it('changes on the plugin’s page, and says what its files use', async () => {
    const device = fakeDevice(sambaOnInternalStorage);
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    const storage = within(section('RasBeacon internal storage'));
    expect(storage.getByText(/Its files use 0.4 GB now/)).toBeTruthy();
    const size = storage.getByRole('textbox', { name: /^Size/ });
    await user.clear(size);
    // A comma works as the decimal point too.
    await user.type(size, '1,5');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.savePluginSettings).toHaveBeenCalledWith('samba', expect.objectContaining({ storageMb: 1536 }));
  });

  // On RasBeacon's internal storage the size is required: the plugin's files are kept in it.
  it('needs a size on internal storage, and says so under the field', async () => {
    const device = fakeDevice(sambaOnInternalStorage);
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    const storage = within(section('RasBeacon internal storage'));
    const size = storage.getByRole('textbox', { name: /^Size/ });
    await user.clear(size);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await storage.findByText('Enter a size: its files are kept in this space.')).toBeTruthy();
    await user.type(size, '.');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await storage.findByText('Enter a size in GB, like 2 or 1.5.')).toBeTruthy();
    expect(device.savePluginSettings).not.toHaveBeenCalled();
  });

  // On a USB drive the files are on the drive: no internal size to give, and the plugin's own
  // setting about the drive (Samba's optional size limit) shows instead.
  it('is left out with a USB drive, where the drive’s size limit shows instead', async () => {
    // The demo's Samba keeps its files on a USB drive, and has the drive's size limit.
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    expect(screen.queryByRole('heading', { name: 'RasBeacon internal storage' })).toBeNull();
    expect(screen.getByLabelText(/^Size limit/)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.savePluginSettings).toHaveBeenCalledWith(
      'samba',
      expect.not.objectContaining({ storageMb: expect.anything() }),
    );
    // Choosing internal storage swaps them.
    await user.selectOptions(await screen.findByLabelText('Drive'), 'RasBeacon internal storage');
    expect(screen.getByRole('heading', { name: 'RasBeacon internal storage' })).toBeTruthy();
    expect(screen.queryByLabelText(/^Size limit/)).toBeNull();
  });

  it('has no field for a plugin with a fixed size', () => {
    const device = fakeDevice();
    renderScreen(<Pages />, device, '/plugins/adguard-home');
    expect(screen.queryByRole('heading', { name: 'RasBeacon internal storage' })).toBeNull();
  });

  it('shows without a limit when RasBeacon didn’t give one', () => {
    const device = fakeDevice((router) => {
      sambaOnInternalStorage(router);
      const samba = router.plugins.plugins.find((p) => p.id === 'samba')!;
      samba.storageMaxMb = undefined;
      samba.storageUsedMb = undefined;
    });
    renderScreen(<Pages />, device, '/plugins/samba');
    expect(within(section('RasBeacon internal storage')).getByText('At least 0.5 GB.')).toBeTruthy();
  });
});

describe('System → Storage', () => {
  it('shows the plugins’ data as a part of its own, and each plugin’s size', () => {
    renderScreen(<SystemScreen />, fakeDevice(), '/system');
    const storage = within(section('Storage'));
    expect(storage.getByText('Plugins')).toBeTruthy();
    expect(storage.getByText('2.5 GB')).toBeTruthy();
    expect(storage.getByText('Plugins: AdGuard Home 0.5 GB, Samba 2.0 GB.')).toBeTruthy();
    // 10 GB less RasBeacon (3.8), settings (2.4) and plugins (2.5).
    expect(storage.getByText('1.3 GB of 10.0 GB free')).toBeTruthy();
  });

  it('has no Plugins part without plugins', () => {
    renderScreen(
      <SystemScreen />,
      fakeDevice((router) => {
        router.status.storage.plugins = [];
      }),
      '/system',
    );
    expect(within(section('Storage')).queryByText('Plugins')).toBeNull();
  });
});
