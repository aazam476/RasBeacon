// System page: changing the admin password with the current one.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

/** Opens the sheet and fills it in. */
async function fillIn(user: ReturnType<typeof renderScreen>['user'], current: string, next: string, again: string) {
  await user.click(screen.getByRole('button', { name: 'Change admin password' }));
  const sheet = within(screen.getByRole('dialog', { name: 'Admin password' }));
  await user.type(sheet.getByLabelText('Current password'), current);
  await user.type(sheet.getByLabelText('New password'), next);
  await user.type(sheet.getByLabelText('Type it again'), again);
  return sheet;
}

describe('admin password', () => {
  it('sends the current password and the new one typed twice, then says it changed', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    const sheet = await fillIn(user, 'tangerine-orbit-42', 'plum-harbor-sunrise-7', 'plum-harbor-sunrise-7');
    await user.click(sheet.getByRole('button', { name: 'Change' }));
    expect(device.changeAdminPassword).toHaveBeenCalledWith({
      currentPassword: 'tangerine-orbit-42',
      newPassword: 'plum-harbor-sunrise-7',
      newPasswordConfirm: 'plum-harbor-sunrise-7',
    });
    expect(await screen.findByText('Admin password changed.')).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: 'Admin password' })).toBeNull();
  });

  it('keeps the sheet open with each problem next to its field', async () => {
    const device = fakeDevice(undefined, {
      changeAdminPassword: async () => {
        throw new FieldErrors({
          currentPassword: 'Wrong password.',
          newPasswordConfirm: "The two passwords don't match.",
        });
      },
    });
    const { user } = renderScreen(<SystemScreen />, device);
    const sheet = await fillIn(user, 'guess', 'plum-harbor-sunrise-7', 'plum-harbor-sunrise-8');
    await user.click(sheet.getByRole('button', { name: 'Change' }));
    expect(await sheet.findByText('Wrong password.')).toBeTruthy();
    expect(sheet.getByLabelText('Current password').getAttribute('aria-invalid')).toBe('true');
    expect(sheet.getByLabelText('Type it again').getAttribute('aria-invalid')).toBe('true');
    expect(screen.queryByText('Admin password changed.')).toBeNull();
  });

  it('Cancel changes nothing', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Change admin password' }));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }));
    expect(device.changeAdminPassword).not.toHaveBeenCalled();
  });
});
