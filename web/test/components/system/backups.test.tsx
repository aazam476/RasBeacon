// System page: backing up and restoring.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

describe('backing up', () => {
  it('asks only for the backup’s password and how much of the plugins to keep', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Back up settings' }));
    const sheet = screen.getByRole('dialog');
    expect(within(sheet).queryByLabelText(/Admin password/)).toBeNull();

    await user.type(within(sheet).getByLabelText('Password for the backup'), 'backup-pass-42');
    await user.click(within(sheet).getByRole('radio', { name: /Plugin data/ }));
    await user.click(within(sheet).getByRole('button', { name: 'Back up' }));
    expect(device.backUp).toHaveBeenCalledWith('backup-pass-42', 'data');
  });

  it('keeps only the settings by default', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Back up settings' }));
    await user.type(screen.getByLabelText('Password for the backup'), 'backup-pass-42');
    await user.click(screen.getByRole('button', { name: 'Back up' }));
    expect(device.backUp).toHaveBeenCalledWith('backup-pass-42', 'settings');
  });

  it('doesn’t offer plugin choices without plugins', async () => {
    const device = fakeDevice((router) => {
      router.plugins.plugins = [];
    });
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Back up settings' }));
    expect(screen.queryByRole('radio', { name: /Everything/ })).toBeNull();
  });
});

describe('restoring', () => {
  it('sends the file and its password, without the admin password', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Restore from a backup' }));
    const sheet = screen.getByRole('dialog');
    expect(within(sheet).queryByLabelText(/Admin password/)).toBeNull();
    expect(within(sheet).getByText(/Installed plugins are removed/)).toBeTruthy();

    const file = new File(['x'], 'rasbeacon-backup.rbbackup');
    await user.upload(within(sheet).getByLabelText(/^Backup file/), file);
    await user.type(within(sheet).getByLabelText("The backup's password"), 'backup-pass-42');
    await user.click(within(sheet).getByRole('button', { name: 'Restore' }));
    expect(device.restore).toHaveBeenCalledWith(file, 'backup-pass-42');
  });
});
