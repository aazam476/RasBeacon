// System page: the running version, news of a newer one, and installing an update file.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { UpdateStatus } from '@/data/types';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

function renderSoftware(update: Partial<UpdateStatus>, actions: Parameters<typeof fakeDevice>[1] = {}) {
  const device = fakeDevice((router) => {
    router.status.update = { ...router.status.update, ...update };
  }, actions);
  return { device, ...renderScreen(<SystemScreen />, device) };
}

describe('software', () => {
  it('names the board the image is for', () => {
    renderSoftware({ board: 'rpi3' });
    expect(screen.getByText('For Raspberry Pi Zero 2 W, 3B and 3B+')).toBeTruthy();
  });

  it('announces a newer version with a link to its release', () => {
    renderSoftware({ latestVersion: '0.3.0', latestUrl: 'https://github.com/example/releases/v0.3.0' });
    expect(screen.getByText('Version 0.3.0 is available')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Release page' }).getAttribute('href')).toBe(
      'https://github.com/example/releases/v0.3.0',
    );
  });

  it('says when a newer version comes after the one offered', () => {
    renderSoftware({ latestVersion: '0.3.0', newerVersion: '0.4.0' });
    expect(screen.getByText('After this, RasBeacon offers 0.4.0, which needs this version first.')).toBeTruthy();
  });

  it('says nothing more when the offered version is the newest', () => {
    renderSoftware({ latestVersion: '0.3.0' });
    expect(screen.queryByText(/After this, RasBeacon offers/)).toBeNull();
  });

  it('offers to finish an installed update that RasBeacon couldn’t restart into, after asking', async () => {
    const message = "Update v0.3.0 is installed, but RasBeacon couldn't restart into it: Too many arguments.";
    const { device, user } = renderSoftware({ restartPending: true, message });
    const notice = screen.getByRole('status', { name: '' });
    expect(within(notice).getByText('Restart to finish the update')).toBeTruthy();
    expect(within(notice).getByText(message)).toBeTruthy();
    // The reason shows once, in the notice, not again as the last update's result.
    expect(screen.queryByText('Last update')).toBeNull();
    await user.click(within(notice).getByRole('button', { name: 'Restart' }));
    expect(device.restartIntoUpdate).not.toHaveBeenCalled();
    await user.click(
      within(screen.getByRole('dialog', { name: 'Restart RasBeacon?' })).getByRole('button', { name: 'Restart' }),
    );
    expect(device.restartIntoUpdate).toHaveBeenCalledOnce();
  });

  it('shows no restart notice when nothing waits', () => {
    renderSoftware({ message: 'Updated to 0.2.0.' });
    expect(screen.queryByText('Restart to finish the update')).toBeNull();
    expect(screen.getByText('Last update')).toBeTruthy();
  });

  it('gives the steps for a version that needs a fresh install, with Back up settings', async () => {
    const { user } = renderSoftware({ reflash: { version: '1.0.0', minFromVersion: '0.9.0' } });
    expect(screen.getByText('RasBeacon 1.0.0 needs a fresh install')).toBeTruthy();
    // The notice comes first on the page; the Backup group further down has the same button.
    await user.click(screen.getAllByRole('button', { name: 'Back up settings' })[0]);
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('shows the last update’s result and a failed check', () => {
    renderSoftware({ message: 'Updated to 0.2.0.', checkError: 'GitHub did not answer.' });
    expect(screen.getByText('Updated to 0.2.0.')).toBeTruthy();
    expect(screen.getByText('GitHub did not answer.')).toBeTruthy();
  });

  it('says an installing update takes devices offline for a while', () => {
    renderSoftware({ installing: true });
    expect(screen.getByText('Installing an update…')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Install update' })).toHaveProperty('disabled', true);
  });

  it('explains how one-system boards move to a new version', () => {
    renderSoftware({ singleSystem: true, canInstall: false });
    expect(screen.getByText('New versions are flashed')).toBeTruthy();
    expect(screen.queryByLabelText(/Update file/)).toBeNull();
  });

  it('says an image without a signing key can’t install updates', () => {
    renderSoftware({ canInstall: false });
    expect(screen.getByText("This image can't install updates")).toBeTruthy();
  });

  it('installs a chosen file after asking, and says it is restarting', async () => {
    const { device, user } = renderSoftware({ canInstall: true, installing: false });
    const file = new File(['x'], 'rasbeacon-0.3.0-rpi64.rbupdate');
    expect(screen.getByRole('button', { name: 'Install update' })).toHaveProperty('disabled', true);
    await user.upload(screen.getByLabelText(/Update file/), file);
    await user.click(screen.getByRole('button', { name: 'Install update' }));
    const alert = within(screen.getByRole('dialog', { name: 'Install the update?' }));
    await user.click(alert.getByRole('button', { name: 'Install' }));
    expect(device.installUpdate).toHaveBeenCalledWith(file);
    expect(await screen.findByText(/Update installed. RasBeacon is restarting/)).toBeTruthy();
  });

  it('shows why an update file was refused', async () => {
    const { user } = renderSoftware(
      { canInstall: true, installing: false },
      {
        installUpdate: async () => {
          throw new Error('The update is not signed by RasBeacon.');
        },
      },
    );
    await user.upload(screen.getByLabelText(/Update file/), new File(['x'], 'bad.rbupdate'));
    await user.click(screen.getByRole('button', { name: 'Install update' }));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Install' }));
    expect(await screen.findByText('The update is not signed by RasBeacon.')).toBeTruthy();
  });

  it('saves the notification address', async () => {
    const { device, user } = renderSoftware({ checkHours: 24 });
    await user.clear(screen.getByLabelText(/^Address/));
    await user.type(screen.getByLabelText(/^Address/), ' https://ntfy.sh/trip ');
    const form = screen.getByLabelText(/^Address/).closest('form')!;
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(device.saveUpdateSettings).toHaveBeenCalledWith(
      expect.objectContaining({ notifyUrl: 'https://ntfy.sh/trip' }),
    );
  });
});
