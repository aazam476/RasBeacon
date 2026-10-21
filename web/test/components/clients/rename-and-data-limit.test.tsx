// Clients page: renaming a device and the data limit.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { fakeDevice, renderScreen } from '../render';

describe('renaming a device', () => {
  it('names the MAC address it is kept for, and won’t save a blank name', async () => {
    const device = fakeDevice();
    const client = device.status.data!.clients[0];
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    await user.click(screen.getAllByRole('button', { name: /^Rename / })[0]);
    const sheet = screen.getByRole('dialog');
    expect(within(sheet).getByText(client.mac)).toBeTruthy();

    const field = within(sheet).getByRole('textbox', { name: 'Name' });
    await user.clear(field);
    expect(within(sheet).getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
    await user.type(field, "Ali's phone");
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(device.renameClient).toHaveBeenCalledWith(client.mac, "Ali's phone");
  });
});

describe('data limit', () => {
  it('saves a limit without a metered connection', async () => {
    const device = fakeDevice((router) => {
      router.usageSettings = { ...router.usageSettings, limitMb: 0 };
    });
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    await user.selectOptions(screen.getByLabelText('Limit per device'), '2000');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveUsageLimit).toHaveBeenCalledWith(2000);
    expect(screen.getByText(/When a device uses up its data, its traffic is blocked/)).toBeTruthy();
  });
});
