// Clients page: each device's controls, the filters, the empty page and the data counts.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { fakeDevice, openRow, renderScreen } from '../render';

function renderClients(change?: (router: MockRouter) => void) {
  const device = fakeDevice(change);
  return { device, ...renderScreen(<ClientsScreen />, device, '/clients') };
}

/** The card of the device called `name`. */
function card(name: string): HTMLElement {
  return screen.getByRole('heading', { level: 2, name }).closest('section')!;
}

describe('a device’s controls', () => {
  it('pauses its internet with the switch, and disconnects it', async () => {
    const { device, user } = renderClients();
    const client = device.status.data!.clients.find((c) => !c.wired && c.internetAccess)!;
    const name = client.name || client.hostname;
    await user.click(within(card(name)).getByRole('switch', { name: `Internet access for ${name}` }));
    expect(device.setClientInternetAccess).toHaveBeenCalledWith(client.mac, false);
    await user.click(within(card(name)).getByRole('button', { name: 'Disconnect' }));
    expect(device.disconnectClient).toHaveBeenCalledWith(client.mac);
  });

  it('a paused device can have its lease flushed instead', async () => {
    const { device, user } = renderClients((router) => {
      router.status.clients[0].internetAccess = false;
    });
    const client = device.status.data!.clients[0];
    const name = client.name || client.hostname;
    const controls = within(card(name));
    expect(controls.getByText(/Internet access paused/)).toBeTruthy();
    await user.click(controls.getByRole('button', { name: 'Flush lease' }));
    expect(device.flushClientLease).toHaveBeenCalledWith(client.mac);
  });

  it('a device over the data limit can’t be let back in with its switch', () => {
    const { device } = renderClients((router) => {
      router.status.clients[0].internetAccess = false;
      router.status.clients[0].limitReached = true;
    });
    const client = device.status.data!.clients[0];
    const name = client.name || client.hostname;
    expect(within(card(name)).getAllByText(/Data limit reached/).length).toBeGreaterThan(0);
    expect(within(card(name)).getByRole('switch')).toHaveProperty('disabled', true);
  });
});

describe('filters', () => {
  it('Paused shows only the paused devices, with its count', async () => {
    const { device, user } = renderClients((router) => {
      router.status.clients.forEach((c, i) => (c.internetAccess = i !== 0));
      router.status.guest.clients.forEach((c) => (c.internetAccess = true));
    });
    const paused = device.status.data!.clients[0];
    await user.click(screen.getByRole('radio', { name: 'Paused (1)' }));
    // One device card, then the page's Data use group.
    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual([
      paused.name || paused.hostname,
      'Data use',
    ]);
  });
});

describe('no devices', () => {
  it('says which networks devices join', () => {
    renderClients((router) => {
      router.status.clients = [];
      router.status.guest.clients = [];
    });
    expect(screen.getByText(/RasBeacon-7162 or the guest network/)).toBeTruthy();
  });
});

describe('data counts', () => {
  it('says how many devices used up their data', () => {
    renderClients((router) => {
      router.usageSettings = { ...router.usageSettings, limitMb: 1000, devicesPaused: 2 };
    });
    expect(screen.getByText(/2 devices have used up their data, so their traffic is blocked/)).toBeTruthy();
  });

  it('resets the counts after asking', async () => {
    const { device, user } = renderClients();
    await user.click(screen.getByRole('button', { name: 'Reset counts' }));
    const alert = within(screen.getByRole('dialog', { name: 'Reset the counts?' }));
    await user.click(alert.getByRole('button', { name: 'Reset' }));
    expect(device.resetUsage).toHaveBeenCalledOnce();
  });

  it('keeps a limit set some other way in the menu', () => {
    renderClients((router) => {
      router.usageSettings = { ...router.usageSettings, limitMb: 1234 };
    });
    expect((screen.getByLabelText('Limit per device') as HTMLSelectElement).value).toBe('1234');
  });
});

describe('swap file', () => {
  it('is on the System page; saving a new size asks the router', async () => {
    const { SystemScreen } = await import('@/screens/system/SystemScreen');
    const device = fakeDevice((router) => {
      router.swapSettings = { ...router.swapSettings, sizeGb: 0, maxGb: 4 };
    });
    const { user } = renderScreen(<SystemScreen />, device);
    const row = within(await openRow(user, 'Swap file'));
    await user.selectOptions(row.getByLabelText('Size'), '2');
    await user.click(row.getByRole('button', { name: /Save|Make/ }));
    expect(device.saveSwapSettings).toHaveBeenCalledWith(2);
  });

  it('says when the card is too full for one', async () => {
    const { SystemScreen } = await import('@/screens/system/SystemScreen');
    const device = fakeDevice((router) => {
      router.swapSettings = { ...router.swapSettings, sizeGb: 0, maxGb: 0 };
    });
    renderScreen(<SystemScreen />, device);
    expect(screen.getByText(/The card is too full for one\./)).toBeTruthy();
  });
});
