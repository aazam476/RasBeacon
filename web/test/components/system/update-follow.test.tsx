// System page: following an update through RasBeacon's restart. While it restarts, the page says
// so; when RasBeacon answers on the new version, the page loads again and says the update is
// complete; when the update failed, the reason shows in red where the progress was.
import { screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UpdateStatus } from '@/data/types';
import { reloadPage } from '@/lib/navigate';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

// jsdom can't load the page again: the test only checks that it was asked to.
vi.mock('@/lib/navigate', async (original) => ({
  ...(await original<typeof import('@/lib/navigate')>()),
  reloadPage: vi.fn(),
}));

/** The System page of a router running `version`, with these update details. */
function renderSystem(version: string, update: Partial<UpdateStatus>) {
  const device = fakeDevice((router) => {
    router.status.version = version;
    router.status.update = { ...router.status.update, canInstall: true, ...update };
  });
  return { device, ...renderScreen(<SystemScreen />, device) };
}

beforeEach(() => sessionStorage.clear());
afterEach(() => vi.mocked(reloadPage).mockClear());

describe('following an update', () => {
  it('shows why the last update failed, in red, without a "Last update" row', () => {
    const message = "The update to v0.4.0 didn't start correctly, so RasBeacon went back to v0.3.0.";
    renderSystem('v0.3.0', { message, failed: true });
    expect(screen.getByText(message).closest('[role="alert"]')).toBeTruthy();
    expect(screen.queryByText('Last update')).toBeNull();
  });

  it('waits for the restart after installing a file, then loads the page again on the new version', async () => {
    const { device, user, rerender, unmount } = renderSystem('v0.3.0', {});
    await user.upload(screen.getByLabelText(/Update file/), new File(['x'], 'rasbeacon-0.4.0-rpi64.rbupdate'));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Install' }));
    expect(await screen.findByText(/RasBeacon is restarting to try it; this page reloads by itself/)).toBeTruthy();
    expect(reloadPage).not.toHaveBeenCalled();

    // RasBeacon answers again, on the new version.
    device.status.data!.version = 'v0.4.0';
    rerender(<SystemScreen />);
    expect(reloadPage).toHaveBeenCalledOnce();

    // The page as it loads again says the update is complete.
    unmount();
    renderSystem('v0.4.0', {});
    expect(screen.getByText('Update completed. RasBeacon is now on v0.4.0.')).toBeTruthy();
    expect(screen.queryByText(/RasBeacon is restarting/)).toBeNull();
  });

  it('follows an update installing from elsewhere, and stops waiting when it fails', () => {
    const { device, rerender } = renderSystem('v0.3.0', { installing: true, message: 'Installing an update…' });
    Object.assign(device.status.data!.update, {
      installing: false,
      restarting: true,
      message: 'Update v0.4.0 installed. RasBeacon is restarting to try it (about 2 minutes).',
    });
    rerender(<SystemScreen />);
    expect(screen.getByText(/RasBeacon is restarting to try it; this page reloads by itself/)).toBeTruthy();

    Object.assign(device.status.data!.update, {
      restarting: false,
      failed: true,
      message: 'The update was not installed: no room left.',
    });
    rerender(<SystemScreen />);
    expect(screen.getByText('The update was not installed: no room left.')).toBeTruthy();
    expect(screen.queryByText(/RasBeacon is restarting to try it/)).toBeNull();
    expect(reloadPage).not.toHaveBeenCalled();
  });

  it('says nothing about a finished update on a later visit', () => {
    sessionStorage.setItem('rasbeacon.update.to', 'v0.4.0');
    renderSystem('v0.4.0', {}).unmount();
    renderSystem('v0.4.0', {});
    expect(screen.queryByText(/Update completed/)).toBeNull();
  });
});
