// System page: the Wi-Fi radios, changing what each one does, and keeping (or not) the change.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import { FieldErrors } from '@/data/errors';
import type { DeviceActions } from '@/data/device';
import { StatusScreen } from '@/screens/status/StatusScreen';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen, section } from '../render';

const secondMac = '00:0a:52:09:f1:7e';

/** The sample router with a second adapter that can run a hotspot. */
function withSecondAdapter(router: MockRouter) {
  router.status.radios.push({
    interface: 'wlan2',
    mac: secondMac,
    phy: 'phy2',
    driver: 'mt7921u',
    isUsb: true,
    usbId: '0e8d:7961',
    supportsAp: true,
    maxAps: 4,
    role: 'unused',
  });
}

function renderSystem(change?: (router: MockRouter) => void, actions: Partial<DeviceActions> = {}) {
  const device = fakeDevice(change, actions);
  return { device, ...renderScreen(<SystemScreen />, device, '/system') };
}

describe('Wi-Fi radios', () => {
  it('lists each radio with its job', () => {
    renderSystem(withSecondAdapter);
    const radios = within(section('Wi-Fi radios'));
    expect(radios.getByText('Built-in Wi-Fi')).toBeTruthy();
    expect(radios.getByText('USB adapter 0e8d:7612')).toBeTruthy();
    expect(radios.getByText('Internet')).toBeTruthy();
    expect(radios.getByText('Hotspot')).toBeTruthy();
    expect(radios.getByText('Not used')).toBeTruthy();
  });

  it('gives the second adapter the guest network', async () => {
    const { device, user } = renderSystem(withSecondAdapter);
    await user.click(screen.getByRole('button', { name: 'Change what each radio does' }));
    const sheet = within(screen.getByRole('dialog'));
    // Automatic says what it picks.
    expect(sheet.getByText(/Automatic: Hotspot\./)).toBeTruthy();
    expect((sheet.getByRole('button', { name: 'Save and restart' }) as HTMLButtonElement).disabled).toBe(true);
    await user.selectOptions(sheet.getByLabelText('USB adapter 0e8d:7961'), 'guest');
    await user.click(sheet.getByRole('button', { name: 'Save and restart' }));
    expect(device.saveRadioRoles).toHaveBeenCalledWith({ [secondMac]: 'guest' });
  });

  it('warns about the built-in Wi-Fi as the hotspot, and refuses two hotspots', async () => {
    const { user } = renderSystem(withSecondAdapter);
    await user.click(screen.getByRole('button', { name: 'Change what each radio does' }));
    const sheet = within(screen.getByRole('dialog'));
    await user.selectOptions(sheet.getByLabelText('Built-in Wi-Fi'), 'hotspot');
    expect(sheet.getByText('The built-in Wi-Fi holds only about 8 devices as a hotspot.')).toBeTruthy();
    await user.selectOptions(sheet.getByLabelText('USB adapter 0e8d:7612'), 'hotspot');
    expect(sheet.getByText('Give each job to one radio only.')).toBeTruthy();
    expect((sheet.getByRole('button', { name: 'Save and restart' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('offers no hotspot to a radio that can’t run one', async () => {
    const { user } = renderSystem((router) => {
      withSecondAdapter(router);
      router.status.radios[2].supportsAp = false;
    });
    await user.click(screen.getByRole('button', { name: 'Change what each radio does' }));
    const options = within(within(screen.getByRole('dialog')).getByLabelText('USB adapter 0e8d:7961'));
    expect(options.queryByRole('option', { name: 'Hotspot' })).toBeNull();
    expect(options.getByRole('option', { name: 'Internet' })).toBeTruthy();
  });

  it('shows the router’s answer', async () => {
    const { user } = renderSystem(withSecondAdapter, {
      saveRadioRoles: async () => {
        throw new FieldErrors({ roles: 'One of these radios isn’t plugged in any more. Reload the page.' });
      },
    });
    await user.click(screen.getByRole('button', { name: 'Change what each radio does' }));
    const sheet = within(screen.getByRole('dialog'));
    await user.selectOptions(sheet.getByLabelText('USB adapter 0e8d:7961'), 'unused');
    await user.click(sheet.getByRole('button', { name: 'Save and restart' }));
    expect(await sheet.findByText(/isn’t plugged in any more/)).toBeTruthy();
  });

  it('can’t be changed with one radio', () => {
    renderSystem((router) => (router.status.radios = [router.status.radios[1]]));
    expect(screen.queryByRole('button', { name: 'Change what each radio does' })).toBeNull();
  });
});

describe('keeping new radio jobs', () => {
  const pending = (router: MockRouter) => {
    withSecondAdapter(router);
    router.status.radios[2] = { ...router.status.radios[2], role: 'guest', chosen: true };
    router.status.radioRolesPending = 161;
  };

  it('asks on the System page, and keeps them', async () => {
    const { device, user } = renderSystem(pending);
    expect(screen.getByText('Keep the radios’ new jobs?')).toBeTruthy();
    expect(screen.getByText(/goes back to the jobs from before in 2 min 41 s/)).toBeTruthy();
    // Nothing else changes until this one is settled.
    expect((screen.getByRole('button', { name: 'Change what each radio does' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    await user.click(screen.getByRole('button', { name: 'Keep' }));
    expect(device.keepRadioRoles).toHaveBeenCalled();
  });

  it('asks on the Status page too, and goes back', async () => {
    const device = fakeDevice(pending);
    const { user } = renderScreen(<StatusScreen />, device, '/');
    await user.click(screen.getByRole('button', { name: 'Go back' }));
    expect(device.revertRadioRoles).toHaveBeenCalled();
  });

  it('isn’t there when nothing waits', () => {
    renderSystem();
    expect(screen.queryByText('Keep the radios’ new jobs?')).toBeNull();
  });
});
