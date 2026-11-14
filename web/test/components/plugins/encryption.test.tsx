// A plugin's page: encrypting its USB drive's files, the recovery key, and decrypting them.
import { screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Pages } from '@/App';
import type { MockRouter } from '@/data/demo/mockData';
import { FieldErrors } from '@/data/errors';
import type { PluginEncryption, PluginState } from '@/data/types';
import { fakeDevice, renderScreen, section } from '../render';

/** The sample router with Samba's drive in this encryption state (and Samba in `state`). */
function withEncryption(encryption: PluginEncryption, state?: PluginState) {
  return (router: MockRouter) => {
    const samba = router.plugins.plugins.find((p) => p.id === 'samba')!;
    samba.encryption = encryption;
    if (state) samba.state = state;
  };
}

afterEach(() => vi.unstubAllGlobals());

describe('encrypting a plugin’s drive', () => {
  it('asks first, then turns it on', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Encryption')).getByRole('switch', { name: /Encrypt the drive’s files/ }));
    const sheet = within(screen.getByRole('dialog', { name: 'Encrypt the drive?' }));
    expect(sheet.getByText(/Samba pauses while RasBeacon moves each file/)).toBeTruthy();
    await user.click(sheet.getByRole('button', { name: 'Encrypt' }));
    expect(device.setPluginEncryption).toHaveBeenCalledWith('samba', true);
  });

  it('shows the progress while the files move, with Samba paused', () => {
    renderScreen(
      <Pages />,
      fakeDevice(withEncryption({ state: 'encrypting', done: 120, total: 3010 }, 'converting')),
      '/plugins/samba',
    );
    const encryption = within(section('Encryption'));
    expect(encryption.getByText('120 of 3,010 files')).toBeTruthy();
    expect(encryption.getByRole('meter', { name: 'Encrypting' })).toBeTruthy();
    expect((screen.getByRole('switch', { name: /Running/ }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText('Paused')).toBeTruthy();
  });

  it('says it is counting before the number of files is known', () => {
    renderScreen(<Pages />, fakeDevice(withEncryption({ state: 'decrypting' }, 'converting')), '/plugins/samba');
    expect(within(section('Encryption')).getByText('Counting the files…')).toBeTruthy();
  });

  it('asks to save the recovery key until it is downloaded', async () => {
    const createObjectURL = vi.fn(() => 'blob:key');
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }));
    const device = fakeDevice(withEncryption({ state: 'on' }), {
      pluginRecoveryKey: vi.fn(async () => ({ fileName: 'key.txt', text: 'ABCDE-FGHIJ' })),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    expect(screen.getByText('Save your recovery key')).toBeTruthy();
    await user.click(screen.getAllByRole('button', { name: 'Download recovery key' })[0]);
    expect(device.pluginRecoveryKey).toHaveBeenCalledWith('samba');
    expect(createObjectURL).toHaveBeenCalled();
  });

  it('doesn’t ask once the key is saved, but still offers it', () => {
    renderScreen(<Pages />, fakeDevice(withEncryption({ state: 'on', recoveryKeySaved: true })), '/plugins/samba');
    expect(screen.queryByText('Save your recovery key')).toBeNull();
    expect(within(section('Encryption')).getByRole('button', { name: 'Download recovery key' })).toBeTruthy();
  });

  it('needs the recovery key to turn it off, and says when it is wrong', async () => {
    const device = fakeDevice(withEncryption({ state: 'on', recoveryKeySaved: true }), {
      setPluginEncryption: vi.fn(async (_id: string, _on: boolean, key?: string) => {
        if (key !== 'ABCDE-FGHIJ') throw new FieldErrors({ recoveryKey: 'This isn’t the drive’s recovery key.' });
      }),
    });
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Encryption')).getByRole('switch', { name: /Encrypt the drive’s files/ }));
    const sheet = within(screen.getByRole('dialog', { name: 'Decrypt the drive?' }));
    const decrypt = sheet.getByRole('button', { name: 'Decrypt' }) as HTMLButtonElement;
    expect(decrypt.disabled).toBe(true);
    const field = sheet.getByRole('textbox', { name: 'Recovery key' });
    await user.type(field, 'WRONG');
    await user.click(decrypt);
    expect(await sheet.findByText('This isn’t the drive’s recovery key.')).toBeTruthy();
    await user.clear(field);
    await user.type(field, 'ABCDE-FGHIJ');
    await user.click(decrypt);
    expect(device.setPluginEncryption).toHaveBeenLastCalledWith('samba', false, 'ABCDE-FGHIJ');
  });

  it('goes on with a move that stopped', async () => {
    const device = fakeDevice(withEncryption({ state: 'encrypting', done: 3, total: 9 }, 'failed'));
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Encryption')).getByRole('button', { name: 'Go on encrypting' }));
    expect(device.setPluginEncryption).toHaveBeenCalledWith('samba', true);
  });

  it('asks for the key to go on decrypting', async () => {
    const device = fakeDevice(withEncryption({ state: 'decrypting', done: 3, total: 9 }, 'failed'));
    const { user } = renderScreen(<Pages />, device, '/plugins/samba');
    await user.click(within(section('Encryption')).getByRole('button', { name: 'Go on decrypting' }));
    expect(screen.getByRole('dialog', { name: 'Decrypt the drive?' })).toBeTruthy();
  });

  it('isn’t offered without a drive', () => {
    renderScreen(<Pages />, fakeDevice(), '/plugins/adguard-home');
    expect(screen.queryByRole('heading', { name: 'Encryption' })).toBeNull();
  });
});
