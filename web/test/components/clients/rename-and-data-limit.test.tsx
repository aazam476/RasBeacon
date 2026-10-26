// Clients page: renaming a device and the data limit.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { fakeDevice, renderScreen } from '../render';

describe('renaming a device', () => {
  it('names the MAC address it is kept for, and saves the new name', async () => {
    const device = fakeDevice();
    const client = device.status.data!.clients[0];
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    await user.click(screen.getAllByRole('button', { name: /^Rename / })[0]);
    const sheet = screen.getByRole('dialog');
    expect(within(sheet).getByText(client.mac)).toBeTruthy();

    const field = within(sheet).getByRole('textbox', { name: 'Name' });
    await user.clear(field);
    await user.type(field, "Ali's phone");
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.renameClient).toHaveBeenCalledWith(client.mac, "Ali's phone");
  });

  // Unlike other names, a device's may be cleared: it goes back to the name the device sends.
  it('saves an empty name, to go back to the device’s own', async () => {
    const device = fakeDevice();
    const client = device.status.data!.clients[0];
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    await user.click(screen.getAllByRole('button', { name: /^Rename / })[0]);
    const sheet = screen.getByRole('dialog');
    expect(within(sheet).getByText(/Leave it empty to use the name the device sends/)).toBeTruthy();
    await user.clear(within(sheet).getByRole('textbox', { name: 'Name' }));
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.renameClient).toHaveBeenCalledWith(client.mac, '');
  });
});

describe('data limit', () => {
  it('saves a limit without a metered connection', async () => {
    const device = fakeDevice((router) => {
      router.usageSettings = { ...router.usageSettings, limitMb: 0 };
    });
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    await user.type(screen.getByLabelText('Limit per device'), '2.5');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveUsageLimit).toHaveBeenCalledWith(2500); // GB to MB
    expect(screen.getByText(/When a device uses up its data, its traffic is blocked/)).toBeTruthy();
  });

  it('shows a saved limit in GB or MB, and saves in the chosen unit', async () => {
    const device = fakeDevice((router) => {
      router.usageSettings = { ...router.usageSettings, limitMb: 750 };
    });
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    const amount = screen.getByLabelText<HTMLInputElement>('Limit per device');
    expect(amount.value).toBe('750');
    expect(screen.getByLabelText<HTMLSelectElement>('Unit').value).toBe('MB');
    await user.clear(amount);
    await user.type(amount, '5');
    await user.selectOptions(screen.getByLabelText('Unit'), 'GB');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveUsageLimit).toHaveBeenCalledWith(5000);
  });

  it('checks the amount while it is typed, and an empty field means no limit', async () => {
    const device = fakeDevice((router) => {
      router.usageSettings = { ...router.usageSettings, limitMb: 5000 };
    });
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    const amount = screen.getByLabelText<HTMLInputElement>('Limit per device');
    await user.clear(amount);
    await user.type(amount, 'lots');
    expect(screen.getByText('Enter an amount up to 1,000 GB, or leave it empty for no limit.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
    await user.clear(amount);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveUsageLimit).toHaveBeenCalledWith(0);
  });
});
