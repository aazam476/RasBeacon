// Priority devices: the switch on each device, the order of the list, the Clients page's notice,
// and the internet speed on the Internet page.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import { FieldErrors } from '@/data/errors';
import type { DeviceActions } from '@/data/device';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen } from '../render';

function renderClients(change?: (router: MockRouter) => void, actions: Partial<DeviceActions> = {}) {
  const device = fakeDevice(change, actions);
  return { device, ...renderScreen(<ClientsScreen />, device, '/clients') };
}

/** The card of the device called `name`. */
function card(name: string): HTMLElement {
  return screen.getByRole('heading', { level: 2, name }).closest('section')!;
}

/** Makes the Work laptop a priority device, with speeds or without. */
function laptopFirst(speeds = { uploadKbps: 20_000, downloadKbps: 100_000 }) {
  return (router: MockRouter) => {
    router.status.clients.find((c) => c.name === 'Work laptop')!.priority = true;
    router.qosSpeeds = speeds;
  };
}

describe('priority devices on the Clients page', () => {
  it('turns priority on with the device’s switch', async () => {
    const { device, user } = renderClients();
    await user.click(within(card('Work laptop')).getByRole('switch', { name: 'Priority' }));
    expect(device.setClientPriority).toHaveBeenCalledWith('f4:d4:88:62:3b:01', true);
  });

  it('lists priority devices first, marked', () => {
    renderClients(laptopFirst());
    const devices = within(screen.getByRole('list', { name: 'Devices' })).getAllByRole('heading', { level: 2 });
    expect(devices[0].textContent).toBe('Work laptop');
    // The badge beside the name, and the switch's title.
    expect(within(card('Work laptop')).getAllByText('Priority')).toHaveLength(2);
    expect(within(card('Work laptop')).getByRole('switch', { name: 'Priority' }).getAttribute('aria-checked')).toBe(
      'true',
    );
  });

  it('asks for the internet speed while none is set', () => {
    renderClients(laptopFirst({ uploadKbps: 0, downloadKbps: 0 }));
    expect(screen.getByText('Set your internet speed')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open Internet settings' }).getAttribute('href')).toBe('/internet');
  });

  it('says nothing while it works, or without priority devices', () => {
    renderClients(laptopFirst());
    expect(screen.queryByText('Set your internet speed')).toBeNull();
    renderClients((router) => (router.qosSpeeds = { uploadKbps: 0, downloadKbps: 0 }));
    expect(screen.queryByText('Set your internet speed')).toBeNull();
  });

  it('shows why the queues don’t work', () => {
    const device = fakeDevice(laptopFirst());
    device.qosSettings.data!.problem = 'RasBeacon couldn’t set up its queues.';
    renderScreen(<ClientsScreen />, device, '/clients');
    expect(screen.getByText('Priority devices don’t go first')).toBeTruthy();
    expect(screen.getByText('RasBeacon couldn’t set up its queues.')).toBeTruthy();
  });

  it('shows the router’s answer when there are too many', async () => {
    const { user } = renderClients(undefined, {
      setClientPriority: async () => {
        throw new Error('At most 16 devices can be priority devices.');
      },
    });
    await user.click(within(card('Work laptop')).getByRole('switch', { name: 'Priority' }));
    expect(await screen.findByText(/At most 16 devices/)).toBeTruthy();
  });
});

/** The Internet speed group. */
const speedSection = () => screen.getByRole('heading', { name: 'Internet speed' }).closest('section')!;

describe('the internet speed on the Internet page', () => {
  it('is hidden until there is a priority device or a speed', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    expect(screen.queryByRole('heading', { name: 'Internet speed' })).toBeNull();
  });

  it('shows the speeds and how many devices go first', () => {
    renderScreen(
      <InternetScreen />,
      fakeDevice(laptopFirst({ uploadKbps: 20_500, downloadKbps: 100_000 })),
      '/internet',
    );
    const group = within(speedSection());
    expect((group.getByLabelText(/^Download/) as HTMLInputElement).value).toBe('100');
    expect((group.getByLabelText(/^Upload/) as HTMLInputElement).value).toBe('20.5');
    expect(group.getByText('1 device goes first')).toBeTruthy();
  });

  it('says when priority isn’t working yet', () => {
    renderScreen(<InternetScreen />, fakeDevice(laptopFirst({ uploadKbps: 0, downloadKbps: 0 })), '/internet');
    expect(within(speedSection()).getByText('Not working')).toBeTruthy();
  });

  it('says when there are no priority devices', () => {
    const device = fakeDevice((router) => (router.qosSpeeds = { uploadKbps: 5_000, downloadKbps: 0 }));
    renderScreen(<InternetScreen />, device, '/internet');
    expect(within(speedSection()).getByText('No priority devices')).toBeTruthy();
  });

  it('counts several devices', () => {
    const device = fakeDevice((router) => {
      laptopFirst()(router);
      router.status.clients[0].priority = true;
    });
    renderScreen(<InternetScreen />, device, '/internet');
    expect(within(speedSection()).getByText('2 devices go first')).toBeTruthy();
  });

  it('fills in the last speed test and saves the speeds in kbit/s', async () => {
    const device = fakeDevice((router) => {
      laptopFirst({ uploadKbps: 0, downloadKbps: 0 })(router);
      router.status.speedtest.last = { server: '', ageSeconds: 60, downloadMbps: 94.37, uploadMbps: 21.04, pingMs: 12 };
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const group = within(speedSection());
    await user.click(group.getByRole('button', { name: /Use the last speed test \(94\.4 down, 21 up\)/ }));
    await user.click(group.getByRole('button', { name: 'Save' }));
    expect(device.saveQoSSettings).toHaveBeenCalledWith({ downloadKbps: 94_400, uploadKbps: 21_000 });
    expect(await group.findByText('Saved.')).toBeTruthy();
  });

  it('checks the speeds as they are typed, and shows the router’s answer', async () => {
    const device = fakeDevice(laptopFirst({ uploadKbps: 0, downloadKbps: 0 }), {
      saveQoSSettings: async () => {
        throw new FieldErrors({ uploadKbps: 'Too fast for this.' });
      },
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const group = within(speedSection());
    await user.type(group.getByLabelText(/^Download/), 'fast');
    expect(group.getByText('Use a speed from 1 to 10,000 Mbps, or leave it empty.')).toBeTruthy();
    expect((group.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true);
    await user.clear(group.getByLabelText(/^Download/));
    await user.type(group.getByLabelText(/^Download/), '50,5');
    await user.type(group.getByLabelText(/^Upload/), '9000');
    await user.click(group.getByRole('button', { name: 'Save' }));
    expect(device.saveQoSSettings).toHaveBeenCalledWith({ downloadKbps: 50_500, uploadKbps: 9_000_000 });
    expect(await group.findByText('Too fast for this.')).toBeTruthy();
  });
});
