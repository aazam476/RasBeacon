// Clients page: "Always use this address".
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { fakeDevice, renderScreen } from '../render';

describe('fixed address', () => {
  it('starts from the address the device has now, and saves it', async () => {
    const device = fakeDevice();
    const client = device.status.data!.clients[0];
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    const row = screen.getAllByRole('button', { name: 'Fixed address' })[0];
    expect(row.textContent).toContain('Off');
    await user.click(row);
    const sheet = screen.getByRole('dialog');
    // Off at first: Save waits for a change.
    expect(within(sheet).getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
    await user.click(within(sheet).getByRole('checkbox', { name: 'Always use this address' }));
    expect((within(sheet).getByRole('textbox', { name: 'Address' }) as HTMLInputElement).value).toBe(client.ip);
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.setFixedAddress).toHaveBeenCalledWith(client.mac, client.ip);
  });

  it('checks the address as it is typed, and shows the router’s answer', async () => {
    const device = fakeDevice(undefined, {
      setFixedAddress: async () => {
        throw new FieldErrors({ address: 'The router said no.' });
      },
    });
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    await user.click(screen.getAllByRole('button', { name: 'Fixed address' })[0]);
    const sheet = screen.getByRole('dialog');
    await user.click(within(sheet).getByRole('checkbox', { name: 'Always use this address' }));
    const field = within(sheet).getByRole('textbox', { name: 'Address' });
    await user.clear(field);
    // The sample hotspot is at 192.168.8.1.
    await user.type(field, '192.168.8.1');
    expect(within(sheet).getByText("That is RasBeacon's own address.")).toBeTruthy();
    expect(within(sheet).getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
    await user.clear(field);
    await user.type(field, '192.168.8.77');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText('The router said no.')).toBeTruthy();
  });

  it('turns a fixed address off', async () => {
    const device = fakeDevice((router) => {
      router.status.clients[0].fixedAddress = '192.168.8.50';
    });
    const client = device.status.data!.clients[0];
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    const row = screen.getAllByRole('button', { name: 'Fixed address' })[0];
    expect(row.textContent).toContain('192.168.8.50');
    await user.click(row);
    const sheet = screen.getByRole('dialog');
    await user.click(within(sheet).getByRole('checkbox', { name: 'Always use this address' }));
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.setFixedAddress).toHaveBeenCalledWith(client.mac, '');
  });

  it('isn’t offered for guests', () => {
    const device = fakeDevice((router) => {
      router.status.clients = [];
    });
    renderScreen(<ClientsScreen />, device, '/clients');
    expect(screen.queryByRole('button', { name: 'Fixed address' })).toBeNull();
  });
});
