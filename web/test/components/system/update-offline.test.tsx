// System page without the internet: Update now needs it to download the update, so it is grayed
// out with the reason, like Check now. Updating from a file still works.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

/** RasBeacon offline, with a version it could download and install found by an earlier check. */
function offlineWithAnOffer() {
  return fakeDevice((router) => {
    router.status.uplink.connected = false;
    router.status.uplink.wifi.state = 'searching';
    Object.assign(router.status.update, {
      canInstall: true,
      canCheckOnline: true,
      latestVersion: 'v0.3.0',
      downloadSize: 412_000_000,
    });
  });
}

describe('updating without the internet', () => {
  it('grays out Update now and says why', async () => {
    const device = offlineWithAnOffer();
    const { user } = renderScreen(<SystemScreen />, device);
    const updateNow = screen.getByRole('button', { name: 'Update now' });
    expect(updateNow).toHaveProperty('disabled', true);
    expect(screen.getByText('Needs an internet connection')).toBeTruthy();
    await user.click(updateNow);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(device.downloadUpdate).not.toHaveBeenCalled();
  });

  it('still offers an update from a file', () => {
    renderScreen(<SystemScreen />, offlineWithAnOffer());
    expect(screen.getByRole('button', { name: 'Update from a file' })).toHaveProperty('disabled', false);
  });
});
