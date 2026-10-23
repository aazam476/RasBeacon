// Guests page: the guest network.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { GuestsScreen } from '@/screens/guests/GuestsScreen';
import { fakeDevice, renderScreen } from '../render';

describe('guest network', () => {
  it('saves its name and password', async () => {
    const device = fakeDevice((router) => {
      router.guestSettings = { enabled: false, ssid: 'RasBeacon-Guests', hasPassword: false, supported: true };
    });
    const { user } = renderScreen(<GuestsScreen />, device);
    await user.click(screen.getByLabelText('Turn on the guest network'));
    await user.type(screen.getByLabelText(/^Guest password/), 'visitors-welcome');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveGuestSettings).toHaveBeenCalledWith(
      expect.objectContaining({ enabled: true, ssid: 'RasBeacon-Guests', password: 'visitors-welcome' }),
    );
  });

  it('says when the adapter can’t run one', () => {
    renderScreen(
      <GuestsScreen />,
      fakeDevice((router) => {
        router.guestSettings = { enabled: false, ssid: '', hasPassword: false, supported: false };
      }),
    );
    expect(screen.getByText("This adapter can't run a guest network")).toBeTruthy();
  });

  it('shows the QR code to join it', async () => {
    const device = fakeDevice(undefined, { wifiQr: async () => ({ ssid: 'RasBeacon-Guests', svg: '<svg></svg>' }) });
    const { user } = renderScreen(<GuestsScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Show QR code' }));
    expect(device.wifiQr).toHaveBeenCalledWith('guest');
  });
});
