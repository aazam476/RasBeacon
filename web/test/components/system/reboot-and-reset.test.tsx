// System page: rebooting and erasing everything, each asked first.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, openRow, renderScreen } from '../render';

describe('reboot and factory reset', () => {
  it('reboots after asking', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Reboot' }));
    expect(device.reboot).not.toHaveBeenCalled();
    await user.click(
      within(screen.getByRole('dialog', { name: 'Reboot RasBeacon?' })).getByRole('button', { name: 'Reboot' }),
    );
    expect(device.reboot).toHaveBeenCalledOnce();
  });

  it('erases everything only with the admin password, and after asking again', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    const row = within(await openRow(user, 'Factory reset'));
    const erase = row.getByRole('button', { name: 'Erase everything' });
    expect(erase).toHaveProperty('disabled', true);
    await user.type(row.getByLabelText('Admin password'), 'tangerine-orbit-42');
    await user.click(erase);
    const alert = within(screen.getByRole('dialog', { name: 'Erase everything?' }));
    await user.click(alert.getByRole('button', { name: 'Erase everything' }));
    expect(device.factoryReset).toHaveBeenCalledWith('tangerine-orbit-42');
  });

  it('Cancel keeps everything', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    const row = within(await openRow(user, 'Factory reset'));
    await user.type(row.getByLabelText('Admin password'), 'tangerine-orbit-42');
    await user.click(row.getByRole('button', { name: 'Erase everything' }));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: 'Erase everything?' })).toBeNull();
    expect(device.factoryReset).not.toHaveBeenCalled();
  });
});
