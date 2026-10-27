// Internet from a phone or USB modem: the connection, the list of phones and the metered switch.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { PhoneUplinkStatus } from '@/data/types';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { StatusScreen } from '@/screens/status/StatusScreen';
import { fakeDevice, renderScreen } from '../render';

const pixel: PhoneUplinkStatus = {
  interface: 'usb0',
  kind: 'android',
  name: 'Pixel 8',
  connected: true,
  receivedBytes: 734_000_000,
  sentBytes: 52_000_000,
};

/** The sample router online through the Pixel. */
const onPhone = () =>
  fakeDevice((router) => {
    Object.assign(router.status.uplink, { kind: 'phone', interface: 'usb0', phones: [pixel] });
    router.status.hotspot.metered = true;
  });

describe('phones and modems', () => {
  it('show nothing without one', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    expect(screen.queryByRole('heading', { name: 'Phones and modems' })).toBeNull();
  });

  it('show the phone in use, with its data', () => {
    renderScreen(<InternetScreen />, onPhone(), '/internet');
    expect(screen.getAllByText('Pixel 8').length).toBeGreaterThan(0);
    expect(screen.getByText('Over USB (usb0)')).toBeTruthy();
    expect(screen.getByText('Data used')).toBeTruthy();
    expect(screen.getByText('Connected')).toBeTruthy();
  });

  it('name phones without a name by their kind, and say which wait for an address', () => {
    const device = fakeDevice((router) => {
      router.status.uplink.phones = [
        { ...pixel, interface: 'eth1', kind: 'iphone', name: '', connected: false },
        { ...pixel, interface: 'usb1', kind: 'android', name: '' },
        { ...pixel, interface: 'enx1', kind: 'modem', name: '' },
      ];
    });
    renderScreen(<InternetScreen />, device, '/internet');
    expect(screen.getByText('iPhone')).toBeTruthy();
    expect(screen.getByText('Android phone')).toBeTruthy();
    expect(screen.getByText('USB modem')).toBeTruthy();
    expect(screen.getByText('Waiting for an address')).toBeTruthy();
  });

  it('turns metered off', async () => {
    const device = onPhone();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('switch', { name: 'Treat as metered' }));
    expect(device.setPhoneMetered).toHaveBeenCalledWith(false);
  });

  it('are named on the Status page', () => {
    renderScreen(<StatusScreen />, onPhone(), '/');
    expect(screen.getAllByText(/Pixel 8/).length).toBeGreaterThan(0);
    expect(screen.getByText('Connected through Pixel 8')).toBeTruthy();
    expect(screen.getByText('USB, metered')).toBeTruthy();
  });
});
