// System page: renaming RasBeacon and shutting it down.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

describe('renaming RasBeacon', () => {
  it('checks the name while it is typed and keeps Save gray until it is valid', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Rename RasBeacon' }));
    const sheet = screen.getByRole('dialog');
    const name = within(sheet).getByRole('textbox', { name: 'Name' });

    await user.clear(name);
    await user.type(name, 'my pi');
    expect(within(sheet).getByText('Use only lowercase letters, digits and hyphens.')).toBeTruthy();
    expect(within(sheet).getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);

    await user.clear(name);
    await user.type(name, '-pi');
    expect(within(sheet).getByText("The name can't start or end with a hyphen.")).toBeTruthy();

    await user.clear(name);
    await user.type(name, 'travel-pi');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.saveHostname).toHaveBeenCalledWith('travel-pi');
  });

  it('starts from the current name without ".lan"', async () => {
    const device = fakeDevice((router) => {
      router.status.device.hostname = 'travel-pi.lan';
    });
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Rename RasBeacon' }));
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveProperty('value', 'travel-pi');
  });
});

describe('shutting down', () => {
  it('asks first, and Cancel does nothing', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Shut down' }));
    const alert = screen.getByRole('dialog', { name: 'Shut down RasBeacon?' });
    await user.click(within(alert).getByRole('button', { name: 'Cancel' }));
    expect(device.shutDown).not.toHaveBeenCalled();
  });

  it('shuts down once confirmed', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Shut down' }));
    const alert = screen.getByRole('dialog', { name: 'Shut down RasBeacon?' });
    await user.click(within(alert).getByRole('button', { name: 'Shut down' }));
    expect(device.shutDown).toHaveBeenCalledOnce();
  });
});
