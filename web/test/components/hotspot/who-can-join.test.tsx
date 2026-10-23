// Hotspot page: allowed devices and the device limit.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { fakeDevice, openRow, renderScreen, type FakeDevice } from '../render';

/** The sample data's device that is viewing the page (it's on Wi-Fi). */
function thisDevice(device: FakeDevice) {
  const client = device.status.data!.clients.find((c) => c.isThisDevice);
  if (!client) throw new Error('the sample data has no "This device"');
  return client;
}

/** Renders the Hotspot page and opens "Allowed devices". */
async function openAllowList(device: FakeDevice) {
  const { user } = renderScreen(<HotspotScreen />, device);
  const row = within(await openRow(user, 'Allowed devices'));
  return { user, row };
}

describe('allowed devices', () => {
  it('says who can join when it is off', () => {
    renderScreen(<HotspotScreen />, fakeDevice());
    expect(screen.getByText('Anyone with the password')).toBeTruthy();
  });

  it('warns before locking out the device in use, and adds it with one tap', async () => {
    const device = fakeDevice();
    const { user, row } = await openAllowList(device);
    await user.click(row.getByLabelText('Only allow these devices'));
    expect(row.getByText("This device isn't on the list")).toBeTruthy();

    await user.click(row.getByRole('button', { name: 'Add this device' }));
    expect(row.queryByText("This device isn't on the list")).toBeNull();
    expect(row.getByLabelText<HTMLTextAreaElement>(/^MAC addresses/).value).toBe(thisDevice(device).mac);
  });

  it('asks before saving a list that locks out the device in use', async () => {
    const device = fakeDevice();
    const { user, row } = await openAllowList(device);
    await user.click(row.getByLabelText('Only allow these devices'));
    await user.type(row.getByLabelText(/^MAC addresses/), '3a:1f:22:aa:bb:cc');
    await user.click(row.getByRole('button', { name: 'Save' }));
    const alert = screen.getByRole('dialog', { name: 'Lock out this device?' });
    expect(device.saveAccessSettings).not.toHaveBeenCalled();

    await user.click(within(alert).getByRole('button', { name: 'Save anyway' }));
    expect(device.saveAccessSettings).toHaveBeenCalledWith({
      allowListOn: true,
      allowed: ['3a:1f:22:aa:bb:cc'],
      maxDevices: 0,
    });
  });

  it('saves the list one address per line, without blank lines', async () => {
    const device = fakeDevice();
    const { user, row } = await openAllowList(device);
    await user.click(row.getByLabelText('Only allow these devices'));
    await user.type(row.getByLabelText(/^MAC addresses/), `${thisDevice(device).mac}\n\n3A-1F-22-AA-BB-CC\n`);
    await user.click(row.getByRole('button', { name: 'Save' }));
    expect(device.saveAccessSettings).toHaveBeenCalledWith({
      allowListOn: true,
      allowed: [thisDevice(device).mac, '3A-1F-22-AA-BB-CC'],
      maxDevices: 0,
    });
  });

  it('marks connected devices already on the list', async () => {
    const device = fakeDevice((router) => {
      router.accessSettings = { allowListOn: true, allowed: [router.status.clients[0].mac], maxDevices: 0 };
    });
    const { row } = await openAllowList(device);
    expect(row.getAllByText('Added')).toHaveLength(1);
  });
});

describe('device limit', () => {
  it('saves the chosen limit', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HotspotScreen />, device);
    const row = within(await openRow(user, 'Device limit'));
    await user.selectOptions(row.getByLabelText('At most'), '8');
    await user.click(row.getByRole('button', { name: 'Save' }));
    expect(device.saveAccessSettings).toHaveBeenCalledWith({ allowListOn: false, allowed: [], maxDevices: 8 });
  });
});
