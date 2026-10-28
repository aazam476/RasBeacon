// System page: how often to look for updates, Check now, and notifications.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

describe('updates', () => {
  it('saves how often to check as soon as it changes, keeping the address', async () => {
    const device = fakeDevice((router) => {
      router.updateSettings = { checkHours: 0, notifyUrl: 'https://ntfy.sh/rasbeacon' };
    });
    const { user } = renderScreen(<SystemScreen />, device);
    await user.selectOptions(screen.getByLabelText('Check for new versions'), '24');
    expect(device.saveUpdateSettings).toHaveBeenCalledWith({ checkHours: 24, notifyUrl: 'https://ntfy.sh/rasbeacon' });
  });

  // A refused setting's error has no message of its own, only its field's.
  it('says why the router refused the schedule', async () => {
    const device = fakeDevice(undefined, {
      saveUpdateSettings: () => Promise.reject(new FieldErrors({ checkHours: 'Choose one of the listed times.' })),
    });
    const { user } = renderScreen(<SystemScreen />, device);
    await user.selectOptions(screen.getByLabelText('Check for new versions'), '24');
    expect(await screen.findByText('Choose one of the listed times.')).toBeTruthy();
  });

  it('checks now', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Check now' }));
    expect(device.checkForUpdates).toHaveBeenCalledOnce();
  });

  it('says when a check found nothing newer', () => {
    const device = fakeDevice((router) => {
      Object.assign(router.status.update, { lastChecked: '2026-10-28T01:00:00Z', checkResult: 'up-to-date' });
    });
    renderScreen(<SystemScreen />, device);
    expect(screen.getByText("No update available. You're on the latest version.")).toBeTruthy();
  });

  it('says a test build is not compared with releases', () => {
    const device = fakeDevice((router) => {
      Object.assign(router.status.update, { lastChecked: '2026-10-28T01:00:00Z', checkResult: 'test-build' });
    });
    renderScreen(<SystemScreen />, device);
    expect(screen.getByText(/This is a test build, so it isn't compared with releases/)).toBeTruthy();
    expect(screen.queryByText(/latest version/)).toBeNull();
  });

  it('gives a failed check’s error instead of a result', () => {
    const device = fakeDevice((router) => {
      Object.assign(router.status.update, {
        lastChecked: '2026-10-28T01:00:00Z',
        checkError: 'Couldn’t ask GitHub: GitHub answered 403 Forbidden',
      });
    });
    renderScreen(<SystemScreen />, device);
    expect(screen.getByText('Couldn’t ask GitHub: GitHub answered 403 Forbidden')).toBeTruthy();
    expect(screen.queryByText(/latest version/)).toBeNull();
  });

  it('sends a test to the typed address before it is saved', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    expect(screen.getByRole('button', { name: 'Send a test' })).toHaveProperty('disabled', true);
    await user.type(screen.getByLabelText(/^Address/), 'https://ntfy.sh/my-topic');
    await user.click(screen.getByRole('button', { name: 'Send a test' }));
    expect(device.testUpdateNotification).toHaveBeenCalledWith('https://ntfy.sh/my-topic');
  });
});
