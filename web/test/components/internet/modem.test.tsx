// Internet page: a 4G/5G modem that needs ModemManager.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { ModemStatus } from '@/data/types';
import { FieldErrors } from '@/data/errors';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { StatusScreen } from '@/screens/status/StatusScreen';
import { fakeDevice, renderScreen } from '../render';

const connected: ModemStatus = {
  name: 'Quectel RM520N-GL',
  state: 'connected',
  operator: 'T-Mobile',
  network: '5G',
  signalPercent: 72,
  pinTries: 3,
};

/** The sample router with the modem in `modem`'s state, carrying the internet if connected. */
function withModem(modem: ModemStatus, actions = {}) {
  return fakeDevice((router) => {
    router.status.uplink.modem = modem;
    if (modem.state === 'connected') Object.assign(router.status.uplink, { kind: 'modem', interface: 'wwan0' });
  }, actions);
}

/** The modem's form (the page has other Save buttons). */
const modemForm = () => screen.getByLabelText(/^APN/).closest('form')!;

describe('mobile modem', () => {
  it('shows nothing without one', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    expect(screen.queryByRole('heading', { name: 'Mobile modem' })).toBeNull();
  });

  it('shows the connected modem and its network', () => {
    renderScreen(<InternetScreen />, withModem(connected), '/internet');
    expect(screen.getAllByText('Quectel RM520N-GL').length).toBeGreaterThan(0);
    expect(screen.getByText('Connected')).toBeTruthy();
    expect(screen.getByText('T-Mobile, 5G, 72% signal')).toBeTruthy();
  });

  it('is named on the Status page', () => {
    const device = withModem(connected);
    device.status.data!.hotspot.metered = true;
    renderScreen(<StatusScreen />, device, '/');
    expect(screen.getByText('Connected through Quectel RM520N-GL')).toBeTruthy();
    expect(screen.getByText('5G, metered')).toBeTruthy();
  });

  it.each([
    ['pin-needed', /Enter its PIN below; RasBeacon tries it once\. The SIM has 1 try left\./],
    ['pin-refused', /RasBeacon won’t try that PIN again/],
    ['puk-needed', /unblock it with its PUK code/],
    ['no-sim', /Put a SIM card in the modem/],
  ] as const)('explains %s', (state, text) => {
    renderScreen(<InternetScreen />, withModem({ ...connected, state, pinTries: 1 }), '/internet');
    expect(screen.getByText(text)).toBeTruthy();
  });

  it('says how many PIN tries are left, and shows the last problem', () => {
    renderScreen(
      <InternetScreen />,
      withModem({ ...connected, state: 'searching', pinTries: 2, error: 'No service' }),
      '/internet',
    );
    expect(screen.getByText('Looking for a network')).toBeTruthy();
    expect(screen.getByText('No service')).toBeTruthy();
  });

  it('saves the APN and the PIN', async () => {
    const device = withModem({ ...connected, state: 'pin-needed' });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.type(screen.getByLabelText(/^APN/), 'fast.t-mobile.com');
    await user.type(screen.getByLabelText(/^SIM PIN/), '4711');
    await user.click(within(modemForm()).getByRole('button', { name: 'Save' }));
    expect(device.saveModemSettings).toHaveBeenCalledWith(
      expect.objectContaining({ apn: 'fast.t-mobile.com', pin: '4711', password: '' }),
    );
  });

  it('shows the router’s answer next to the PIN', async () => {
    const device = withModem(
      { ...connected, state: 'pin-needed' },
      {
        saveModemSettings: async () => {
          throw new FieldErrors({ pin: 'A SIM PIN is 4 to 8 digits.' });
        },
      },
    );
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(within(modemForm()).getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('A SIM PIN is 4 to 8 digits.')).toBeTruthy();
  });
});
