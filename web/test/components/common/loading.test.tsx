// What every page shows while its data loads or when loading failed, and when an action fails.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { fakeDevice, renderScreen } from '../render';

describe('loading', () => {
  it('says why the data couldn’t be loaded, with Try again', async () => {
    const device = fakeDevice();
    Object.assign(device, { vpnSettings: { data: undefined, error: "Can't reach RasBeacon." } });
    const { user } = renderScreen(<VpnScreen />, device);
    expect(screen.getByText("Can't reach RasBeacon.")).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(device.reload).toHaveBeenCalledWith('vpnSettings');
  });

  it('shows a failed switch in a message that can be closed', async () => {
    const device = fakeDevice(undefined, {
      setKillSwitch: async () => {
        throw new Error("Can't reach RasBeacon.");
      },
    });
    const { user } = renderScreen(<VpnScreen />, device);
    await user.click(screen.getByRole('switch', { name: /Kill switch/ }));
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getAllByText("Can't reach RasBeacon.").length).toBeGreaterThan(0);
  });
});
