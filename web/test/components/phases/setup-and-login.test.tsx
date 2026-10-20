// First-time setup and logging in.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { LoginScreen } from '@/screens/phases/LoginScreen';
import { SetupScreen } from '@/screens/phases/SetupScreen';
import { fakeDevice, renderScreen } from '../render';

function setupDevice() {
  return fakeDevice((router) => {
    router.setup = { ...router.setup, mode: 'setup', claimed: false };
  });
}

describe('first-time setup', () => {
  it('suggests the hotspot name and password', () => {
    renderScreen(<SetupScreen />, setupDevice());
    expect(screen.getByLabelText('Network name')).toHaveProperty('value', 'RasBeacon-7162');
    expect(screen.getByLabelText(/^Wi-Fi password/)).toHaveProperty('value', 'kq7m-2xvd-h8pt-rn4w');
  });

  // The check itself is a logic test (checkSetupForm); here, its message reaches the right field.
  it('shows a problem next to its field', async () => {
    const device = fakeDevice(
      (router) => {
        router.setup = { ...router.setup, mode: 'setup', claimed: false };
      },
      {
        finishSetup: async () => {
          throw new FieldErrors({ adminPasswordConfirm: "The two passwords don't match." });
        },
      },
    );
    const { user } = renderScreen(<SetupScreen />, device);
    await user.type(screen.getByLabelText('Admin password', { exact: true }), 'tangerine-orbit-42');
    await user.type(screen.getByLabelText('Type it again'), 'different');
    await user.click(screen.getByRole('button', { name: 'Finish setup' }));
    const confirm = screen.getByLabelText('Type it again');
    expect(await screen.findByText("The two passwords don't match.")).toBeTruthy();
    expect(confirm.getAttribute('aria-invalid')).toBe('true');
  });

  it('finishes with the chosen settings', async () => {
    const device = setupDevice();
    const { user } = renderScreen(<SetupScreen />, device);
    await user.type(screen.getByLabelText('Admin password', { exact: true }), 'tangerine-orbit-42');
    await user.type(screen.getByLabelText('Type it again'), 'tangerine-orbit-42');
    await user.click(screen.getByRole('button', { name: 'Finish setup' }));
    expect(device.finishSetup).toHaveBeenCalledWith(
      expect.objectContaining({ adminPassword: 'tangerine-orbit-42', hotspotSsid: 'RasBeacon-7162' }),
    );
  });
  it('says when the setup network closes', () => {
    renderScreen(
      <SetupScreen />,
      fakeDevice((router) => {
        router.setup = { ...router.setup, mode: 'setup', claimed: false, setupClosesInSeconds: 1610 };
      }),
    );
    expect(screen.getByText('The setup network closes in about 27 minutes.')).toBeTruthy();
  });

  it('after finishing, says which network to join and where to go', async () => {
    const device = fakeDevice(
      (router) => {
        router.setup = { ...router.setup, mode: 'setup', claimed: false };
      },
      {
        finishSetup: async () => ({ hotspotSsid: 'Trip-Net', switchInSeconds: 1, adminUrl: 'http://rasbeacon.lan' }),
      },
    );
    const { user } = renderScreen(<SetupScreen />, device);
    await user.type(screen.getByLabelText('Admin password', { exact: true }), 'tangerine-orbit-42');
    await user.type(screen.getByLabelText('Type it again'), 'tangerine-orbit-42');
    await user.click(screen.getByRole('button', { name: 'Finish setup' }));
    expect(await screen.findByRole('heading', { name: 'Setup complete' })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe(
      'The setup network turns off in 1 second, and your hotspot starts.',
    );
    expect(screen.getAllByRole('link', { name: 'http://rasbeacon.lan' })).toHaveLength(2);
  });
});

describe('logging in', () => {
  it('stays logged in unless told not to', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<LoginScreen />, device);
    await user.type(screen.getByLabelText('Admin password'), 'tangerine-orbit-42');
    await user.click(screen.getByRole('button', { name: 'Log in' }));
    expect(device.logIn).toHaveBeenLastCalledWith('tangerine-orbit-42', true);

    await user.click(screen.getByLabelText('Stay logged in'));
    await user.click(screen.getByRole('button', { name: 'Log in' }));
    expect(device.logIn).toHaveBeenLastCalledWith('tangerine-orbit-42', false);
  });

  it('shows the router’s answer to a wrong password', async () => {
    const device = fakeDevice(undefined, {
      logIn: async () => {
        throw new Error('Wrong password.');
      },
    });
    const { user } = renderScreen(<LoginScreen />, device);
    await user.type(screen.getByLabelText('Admin password'), 'nope');
    await user.click(screen.getByRole('button', { name: 'Log in' }));
    expect(await screen.findByText('Wrong password.')).toBeTruthy();
  });
});
