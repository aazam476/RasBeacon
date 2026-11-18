// Plugins: choosing the size of a plugin's data on RasBeacon's own storage, for a plugin whose
// manifest allows it (a test plugin here, File Store, which can also keep its files on a USB
// drive), and seeing it in System → Storage.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import type { MockRouter } from '@/data/demo/mockData';
import {
  createMockPlugins,
  definitionOf,
  installedPlugin,
  previewOf,
  type PluginDefinition,
} from '@/data/demo/plugins';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen, section } from '../render';

/** A plugin whose data size the owner chooses (2 GB suggested), on internal storage or a drive. */
const FILE_STORE: PluginDefinition = {
  ...definitionOf('trip-notes')!,
  id: 'file-store',
  name: 'File Store',
  permissions: ['usb-drive'],
  usesDrive: true,
  driveOptional: true,
  storageMb: 2048,
  storageAdjustable: true,
  shares: undefined,
};

/** The sample router with File Store installed: on a USB drive, or on internal storage. */
function withFileStore(drive?: string) {
  return (router: MockRouter) => {
    router.plugins.plugins.push(
      installedPlugin(FILE_STORE, '10.89.0.9', { ports: {}, settings: {}, drive: drive ?? '' }, 'running'),
    );
  };
}

/** The install sheet's preview for File Store, as a new install. */
function fileStorePreview() {
  return previewOf(FILE_STORE, createMockPlugins(), 'staging-9');
}

describe('the size of a plugin’s data', () => {
  it('is chosen when installing, in GB, with the sizes it may have', async () => {
    const device = fakeDevice(undefined, { previewPluginFile: async () => fileStorePreview() });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'file-store.rba'));
    const sheet = within(await screen.findByRole('dialog', { name: 'Install File Store' }));
    const size = sheet.getByRole('textbox', { name: /^Size/ }) as HTMLInputElement;
    expect(size.value).toBe('2');
    expect(sheet.getByText('From 0.3 to 6 GB.')).toBeTruthy();
    await user.clear(size);
    await user.type(size, '3.5');
    await user.click(sheet.getByRole('button', { name: 'Install' }));
    expect(device.installPlugin).toHaveBeenCalledWith(
      'staging-9',
      expect.objectContaining({ storageMb: 3584, drive: '' }),
      false,
    );
  });

  it('isn’t asked for by an update, which keeps it', async () => {
    const state = createMockPlugins();
    state.plugins.push(installedPlugin(FILE_STORE, '10.89.0.9', { ports: {}, settings: {}, drive: '' }, 'running'));
    const update = previewOf(FILE_STORE, state, 'staging-10');
    const device = fakeDevice(undefined, { previewPluginFile: async () => update });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'file-store.rba'));
    const sheet = within(await screen.findByRole('dialog', { name: 'Update File Store' }));
    expect(sheet.queryByRole('textbox', { name: /^Size/ })).toBeNull();
    await user.click(sheet.getByRole('button', { name: 'Update' }));
    expect(device.installPlugin).toHaveBeenCalledWith(
      'staging-10',
      expect.not.objectContaining({ storageMb: expect.anything() }),
      false,
    );
  });

  it('changes on the plugin’s page, and says what its files use', async () => {
    const device = fakeDevice(withFileStore());
    const { user } = renderScreen(<Pages />, device, '/plugins/file-store');
    const storage = within(section('RasBeacon internal storage'));
    expect(storage.getByText(/Its files use 0.4 GB now/)).toBeTruthy();
    const size = storage.getByRole('textbox', { name: /^Size/ });
    await user.clear(size);
    // A comma works as the decimal point too.
    await user.type(size, '1,5');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.savePluginSettings).toHaveBeenCalledWith('file-store', expect.objectContaining({ storageMb: 1536 }));
  });

  // On RasBeacon's internal storage the size is required: the plugin's files are kept in it.
  it('needs a size on internal storage, and says so under the field', async () => {
    const device = fakeDevice(withFileStore());
    const { user } = renderScreen(<Pages />, device, '/plugins/file-store');
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

  // On a USB drive the files are on the drive: no internal size to give.
  it('is left out with a USB drive, and comes back with internal storage', async () => {
    const device = fakeDevice(withFileStore('6A1F-22C9'));
    const { user } = renderScreen(<Pages />, device, '/plugins/file-store');
    expect(screen.queryByRole('heading', { name: 'RasBeacon internal storage' })).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.savePluginSettings).toHaveBeenCalledWith(
      'file-store',
      expect.not.objectContaining({ storageMb: expect.anything() }),
    );
    await user.selectOptions(await screen.findByLabelText('Drive', { exact: true }), 'RasBeacon internal storage');
    expect(screen.getByRole('heading', { name: 'RasBeacon internal storage' })).toBeTruthy();
  });

  it('has no field for a plugin with a fixed size', () => {
    const device = fakeDevice();
    renderScreen(<Pages />, device, '/plugins/adguard-home');
    expect(screen.queryByRole('heading', { name: 'RasBeacon internal storage' })).toBeNull();
  });

  it('shows without a limit when RasBeacon didn’t give one', () => {
    const device = fakeDevice((router) => {
      withFileStore()(router);
      const store = router.plugins.plugins.find((p) => p.id === 'file-store')!;
      store.storageMaxMb = undefined;
      store.storageUsedMb = undefined;
    });
    renderScreen(<Pages />, device, '/plugins/file-store');
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
    // 10 GB less RasBeacon (3.8), settings (0.3), plugins (2.5) and swap (2.1).
    expect(storage.getByText('1.3 GB of 10.0 GB free')).toBeTruthy();
  });

  it('shows the swap file as a part of its own, last', () => {
    renderScreen(<SystemScreen />, fakeDevice(), '/system');
    const storage = within(section('Storage'));
    const labels = storage.getAllByRole('term').map((term) => term.textContent);
    expect(labels).toEqual(['RasBeacon', 'Settings and logs', 'Plugins', 'Swap']);
    const swap = storage.getByText('Swap').parentElement!;
    expect(within(swap).getByText('2.1 GB')).toBeTruthy();
    // In a color of its own.
    expect(swap.querySelector('.bg-tile-purple')).toBeTruthy();
  });

  it('has no Swap part with swap off', () => {
    renderScreen(
      <SystemScreen />,
      fakeDevice((router) => {
        router.status.storage.swapGb = 0;
      }),
      '/system',
    );
    expect(within(section('Storage')).queryByText('Swap')).toBeNull();
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
