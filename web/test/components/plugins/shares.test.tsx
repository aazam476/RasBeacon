// A plugin's page: its shares (the demo's Samba has three: Files on internal storage, Movies on
// the TRAVEL stick over its quota, and Passports, encrypted, with its key not saved yet).
import { screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Pages } from '@/App';
import type { MockRouter } from '@/data/demo/mockData';
import { DEMO_DRIVES } from '@/data/demo/plugins';
import { FieldErrors } from '@/data/errors';
import type { PluginShare } from '@/data/types';
import { fakeDevice, renderScreen, section } from '../render';

/** Changes one of the sample Samba's shares. */
function withShare(id: string, change: (share: PluginShare) => void) {
  return (router: MockRouter) => {
    change(router.plugins.plugins.find((p) => p.id === 'samba')!.shares!.list.find((s) => s.id === id)!);
  };
}

afterEach(() => vi.unstubAllGlobals());

describe('a plugin’s shares', () => {
  it('lists each share with where it is and what it uses', () => {
    renderScreen(<Pages />, fakeDevice(), '/plugins/samba');
    const shares = within(section('Shares'));
    expect(shares.getByText('Internal storage')).toBeTruthy();
    expect(shares.getByText('412 MB of 2 GB')).toBeTruthy();
    // Over its quota after files were added on a computer: in red, and said.
    expect(shares.getByText('22 of 20 GB').className).toContain('text-red');
    expect(shares.getByText(/over its quota/)).toBeTruthy();
    expect(shares.getByText('38 MB of 1 GB')).toBeTruthy();
  });

  it('says when a share’s drive isn’t plugged in, or its use isn’t measured yet', () => {
    renderScreen(
      <Pages />,
      fakeDevice((router) => {
        withShare('s2', (s) => (s.driveMissing = true))(router);
        withShare('s1', (s) => (s.usedMb = -1))(router);
      }),
      '/plugins/samba',
    );
    const shares = within(section('Shares'));
    expect(shares.getByText(/drive not plugged in/)).toBeTruthy();
    expect(shares.getByText('Measuring…')).toBeTruthy();
  });

  it('starts with none', () => {
    renderScreen(
      <Pages />,
      fakeDevice((router) => {
        router.plugins.plugins.find((p) => p.id === 'samba')!.shares!.list = [];
      }),
      '/plugins/samba',
    );
    expect(within(section('Shares')).getByText('No shares yet')).toBeTruthy();
  });

  it('can’t have more than its manifest allows', () => {
    renderScreen(
      <Pages />,
      fakeDevice((router) => {
        router.plugins.plugins.find((p) => p.id === 'samba')!.shares!.max = 3;
      }),
      '/plugins/samba',
    );
    const add = within(section('Shares')).getByRole('button', { name: /Add a share/ }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    expect(screen.getByText('Samba can have at most 3 shares.')).toBeTruthy();
  });

  it('asks to save an encrypted share’s recovery key until it is downloaded', async () => {
    const createObjectURL = vi.fn(() => 'blob:key');
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }));
    const device = fakeDevice(undefined, {
      pluginRecoveryKey: vi.fn(async () => ({ fileName: 'key.txt', text: 'ABCDE-FGHIJ' })),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    expect(screen.getByText('Save the recovery key of Passports')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Download recovery key' }));
    expect(device.pluginRecoveryKey).toHaveBeenCalledWith('samba', 's3');
    expect(createObjectURL).toHaveBeenCalled();
  });

  it('shows the progress while a share’s files are encrypted', () => {
    renderScreen(
      <Pages />,
      fakeDevice(withShare('s1', (s) => (s.encryption = { state: 'encrypting', done: 120, total: 3010 }))),
      '/plugins/samba',
    );
    const shares = within(section('Shares'));
    expect(shares.getByText('Encrypting: 120 of 3,010 files')).toBeTruthy();
    expect(shares.getByRole('meter', { name: 'Encrypting Files' })).toBeTruthy();
  });

  it('says it is counting before the number of files is known', () => {
    renderScreen(
      <Pages />,
      fakeDevice(withShare('s1', (s) => (s.encryption = { state: 'decrypting' }))),
      '/plugins/samba',
    );
    expect(within(section('Shares')).getByText('Decrypting…')).toBeTruthy();
  });
});

describe('adding a share', () => {
  it('on internal storage: its name, quota in GB and settings', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(screen.getByRole('button', { name: 'Add a share' }));
    const sheet = within(screen.getByRole('dialog', { name: 'Add a share' }));
    expect(sheet.getByText(/Up to 6 GB: RasBeacon keeps 2 GB/)).toBeTruthy();
    await user.type(sheet.getByRole('textbox', { name: 'Name' }), 'Docs');
    // The quota is required.
    await user.click(sheet.getByRole('button', { name: 'Add' }));
    expect(await sheet.findByText('Enter a quota in GB.')).toBeTruthy();
    await user.type(sheet.getByRole('textbox', { name: /^Quota/ }), 'x');
    await user.click(sheet.getByRole('button', { name: 'Add' }));
    expect(await sheet.findByText('Enter a quota in GB.')).toBeTruthy();
    await user.type(sheet.getByRole('textbox', { name: /^Quota/ }), '1.5');
    await user.click(sheet.getByLabelText(/Read-only/));
    await user.click(sheet.getByRole('button', { name: 'Add' }));
    expect(device.addPluginShare).toHaveBeenCalledWith('samba', {
      name: 'Docs',
      drive: '',
      folder: '',
      quotaMb: 1536,
      settings: { readOnly: 'true' },
    });
  });

  it('on a USB drive: its folder follows the name until typed', async () => {
    const device = fakeDevice(undefined, {
      pluginDrives: async () => [
        ...DEMO_DRIVES,
        { uuid: 'B0B0-0001', label: 'OTHER', fsType: 'exfat', size: 1e9, usedBy: 'Photo Frame' },
      ],
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(screen.getByRole('button', { name: 'Add a share' }));
    const sheet = within(screen.getByRole('dialog', { name: 'Add a share' }));
    const storage = await sheet.findByLabelText('Storage');
    // Samba's own drive and a free one are offered; another plugin's isn't.
    await vi.waitFor(() => expect((storage as HTMLSelectElement).disabled).toBe(false));
    expect(within(storage).getByText('Untitled (USB drive)')).toBeTruthy();
    expect(within(storage).queryByText(/OTHER/)).toBeNull();
    await user.selectOptions(storage, 'TRAVEL (USB drive)');
    await user.type(sheet.getByRole('textbox', { name: 'Name' }), 'Photos');
    const folder = sheet.getByRole('textbox', { name: 'Folder on the drive' }) as HTMLInputElement;
    expect(folder.value).toBe('Photos');
    await user.clear(folder);
    await user.type(folder, 'Media/Photos');
    await user.type(sheet.getByRole('textbox', { name: /^Quota/ }), '4');
    await user.click(sheet.getByRole('button', { name: 'Add' }));
    expect(device.addPluginShare).toHaveBeenCalledWith(
      'samba',
      expect.objectContaining({ name: 'Photos', drive: '6A1F-22C9', folder: 'Media/Photos', quotaMb: 4096 }),
    );
  });

  it('can be encrypted from the start', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(screen.getByRole('button', { name: 'Add a share' }));
    const sheet = within(screen.getByRole('dialog', { name: 'Add a share' }));
    await user.type(sheet.getByRole('textbox', { name: 'Name' }), 'Secret');
    await user.type(sheet.getByRole('textbox', { name: /^Quota/ }), '1');
    await user.click(sheet.getByLabelText('Encrypt this share’s files'));
    await user.click(sheet.getByRole('button', { name: 'Add' }));
    expect(device.addPluginShare).toHaveBeenCalledWith(
      'samba',
      expect.objectContaining({ name: 'Secret', encrypt: true }),
    );
  });

  it('shows RasBeacon’s answer under the field it is about', async () => {
    const device = fakeDevice(undefined, {
      addPluginShare: vi.fn(async () => {
        throw new FieldErrors({ name: 'Another share has this name.' });
      }),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(screen.getByRole('button', { name: 'Add a share' }));
    const sheet = within(screen.getByRole('dialog', { name: 'Add a share' }));
    await user.type(sheet.getByRole('textbox', { name: 'Name' }), 'files');
    await user.type(sheet.getByRole('textbox', { name: /^Quota/ }), '1');
    await user.click(sheet.getByRole('button', { name: 'Add' }));
    expect(await sheet.findByText('Another share has this name.')).toBeTruthy();
  });

  it('says when the drives can’t be listed', async () => {
    const device = fakeDevice(undefined, {
      pluginDrives: vi.fn(async () => {
        throw new Error('The USB drives can’t be listed.');
      }),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(screen.getByRole('button', { name: 'Add a share' }));
    expect(await screen.findByText('The USB drives can’t be listed.')).toBeTruthy();
  });
});

describe('changing a share', () => {
  it('saves its name, quota and settings; where it is stays', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Files/ }));
    const sheet = within(screen.getByRole('dialog', { name: 'Files' }));
    expect(sheet.getByText('Internal storage')).toBeTruthy();
    expect(sheet.getByText(/From 0.5 to 8 GB. Its files use 0.4 GB./)).toBeTruthy();
    const name = sheet.getByRole('textbox', { name: 'Name' });
    await user.clear(name);
    await user.type(name, 'Documents');
    const quota = sheet.getByRole('textbox', { name: /^Quota/ });
    await user.clear(quota);
    await user.type(quota, '3');
    await user.click(sheet.getByRole('button', { name: 'Save' }));
    expect(device.savePluginShare).toHaveBeenCalledWith('samba', 's1', {
      name: 'Documents',
      quotaMb: 3072,
      settings: { readOnly: 'false' },
    });
  });

  it('downloads an encrypted share’s key from its sheet', async () => {
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:x'), revokeObjectURL: vi.fn() }));
    const device = fakeDevice(undefined, {
      pluginRecoveryKey: vi.fn(async () => ({ fileName: 'key.txt', text: 'KEY' })),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Passports/ }));
    const sheet = within(screen.getByRole('dialog', { name: 'Passports' }));
    await user.click(sheet.getByRole('button', { name: 'Download recovery key' }));
    expect(device.pluginRecoveryKey).toHaveBeenCalledWith('samba', 's3');
  });
});

describe('encrypting and decrypting a share', () => {
  it('asks first, then encrypts', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Movies/ }));
    await user.click(within(screen.getByRole('dialog', { name: 'Movies' })).getByLabelText(/Encrypt this share/));
    const dialog = within(screen.getByRole('dialog', { name: 'Encrypt Movies?' }));
    await user.click(dialog.getByRole('button', { name: 'Encrypt' }));
    expect(device.setPluginEncryption).toHaveBeenCalledWith('samba', 's2', true);
  });

  it('needs the recovery key to decrypt', async () => {
    const device = fakeDevice(undefined, {
      setPluginEncryption: vi.fn(async (_id: string, _share: string, _on: boolean, key?: string) => {
        if (key !== 'ABCDE-FGHIJ') throw new FieldErrors({ recoveryKey: 'This isn’t the share’s recovery key.' });
      }),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Passports/ }));
    await user.click(within(screen.getByRole('dialog', { name: 'Passports' })).getByLabelText(/Encrypt this share/));
    const sheet = within(screen.getByRole('dialog', { name: 'Decrypt Passports?' }));
    const decrypt = sheet.getByRole('button', { name: 'Decrypt' }) as HTMLButtonElement;
    expect(decrypt.disabled).toBe(true);
    const field = sheet.getByRole('textbox', { name: 'Recovery key' });
    await user.type(field, 'WRONG');
    await user.click(decrypt);
    expect(await sheet.findByText('This isn’t the share’s recovery key.')).toBeTruthy();
    await user.clear(field);
    await user.type(field, 'ABCDE-FGHIJ');
    await user.click(decrypt);
    expect(device.setPluginEncryption).toHaveBeenLastCalledWith('samba', 's3', false, 'ABCDE-FGHIJ');
  });
});

describe('removing a share', () => {
  it('on internal storage: its files go with it', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Files/ }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Files' })).getByRole('button', { name: 'Remove share' }),
    );
    const dialog = within(screen.getByRole('dialog', { name: 'Remove Files?' }));
    expect(dialog.getByText(/Its files are deleted with it/)).toBeTruthy();
    await user.click(dialog.getByRole('button', { name: 'Remove and delete' }));
    expect(device.removePluginShare).toHaveBeenCalledWith('samba', 's1', false);
  });

  it('on a drive: its folder stays, unless deleted too', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Movies/ }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Movies' })).getByRole('button', { name: 'Remove share' }),
    );
    const dialog = within(screen.getByRole('dialog', { name: 'Remove Movies?' }));
    expect(dialog.getByText(/stays on the drive/)).toBeTruthy();
    await user.click(dialog.getByLabelText(/Also delete the folder/));
    await user.click(dialog.getByRole('button', { name: 'Remove and delete' }));
    expect(device.removePluginShare).toHaveBeenCalledWith('samba', 's2', true);
  });

  it('encrypted on a drive: its folder always goes', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Passports/ }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Passports' })).getByRole('button', { name: 'Remove share' }),
    );
    const dialog = within(screen.getByRole('dialog', { name: 'Remove Passports?' }));
    expect(dialog.getByText(/its encrypted files can’t be read without it/)).toBeTruthy();
    expect(dialog.queryByLabelText(/Also delete the folder/)).toBeNull();
  });
});

// A share carried over from Samba 1.0, which shared a whole drive.
describe('a share of a whole drive', () => {
  const wholeDrive = withShare('s2', (s) => {
    s.folder = undefined;
    s.usedMb = 100;
  });

  it('says it is the whole drive', () => {
    renderScreen(<Pages />, fakeDevice(wholeDrive), '/plugins/samba');
    expect(within(section('Shares')).getByText('TRAVEL (whole drive)')).toBeTruthy();
  });

  it('keeps the drive’s files when removed, and doesn’t offer to delete them', async () => {
    const device = fakeDevice(wholeDrive);
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Movies/ }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Movies' })).getByRole('button', { name: 'Remove share' }),
    );
    const dialog = within(screen.getByRole('dialog', { name: 'Remove Movies?' }));
    expect(dialog.getByText(/The drive’s files stay on it\./)).toBeTruthy();
    expect(dialog.queryByLabelText(/Also delete the folder/)).toBeNull();
    await user.click(dialog.getByRole('button', { name: 'Remove' }));
    expect(device.removePluginShare).toHaveBeenCalledWith('samba', 's2', false);
  });

  it('says an encrypted one stays readable with its key', async () => {
    const device = fakeDevice((router) => {
      wholeDrive(router);
      withShare('s2', (s) => (s.encryption = { state: 'on', recoveryKeySaved: true }))(router);
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Shares')).getByRole('button', { name: /^Movies/ }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Movies' })).getByRole('button', { name: 'Remove share' }),
    );
    expect(screen.getByText(/encrypted: you need its recovery key/)).toBeTruthy();
  });
});
