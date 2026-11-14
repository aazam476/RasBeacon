// System page: naming the Wi-Fi radios, with the pencil next to each one's name.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen, section } from '../render';

describe('naming a Wi-Fi radio', () => {
  it('saves a name for the radio, kept by its MAC address', async () => {
    const device = fakeDevice();
    const usb = device.status.data!.radios.find((radio) => radio.isUsb)!;
    const { user } = renderScreen(<SystemScreen />, device, '/system');
    await user.click(screen.getByRole('button', { name: 'Rename USB adapter 0e8d:7612' }));
    const sheet = within(screen.getByRole('dialog'));
    expect(sheet.getByText(usb.mac)).toBeTruthy();
    // Without a name of its own, the field is empty and shows what the radio is called now.
    const field = sheet.getByRole('textbox', { name: 'Name' }) as HTMLInputElement;
    expect(field.value).toBe('');
    expect(field.placeholder).toBe('USB adapter 0e8d:7612');
    await user.type(field, 'Blue adapter');
    await user.click(sheet.getByRole('button', { name: 'Save' }));
    expect(device.renameRadio).toHaveBeenCalledWith(usb.mac, 'Blue adapter');
  });

  it('shows the owner’s name, and an empty one goes back to what the radio is', async () => {
    const device = fakeDevice((router) => {
      router.status.radios.find((radio) => !radio.isUsb)!.name = 'Inside the case';
    });
    const builtIn = device.status.data!.radios.find((radio) => !radio.isUsb)!;
    const { user } = renderScreen(<SystemScreen />, device, '/system');
    const radios = within(section('Wi-Fi radios'));
    expect(radios.getByText('Inside the case')).toBeTruthy();
    expect(radios.queryByText('Built-in Wi-Fi')).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Rename Inside the case' }));
    const sheet = within(screen.getByRole('dialog'));
    expect(sheet.getByText(/Leave it empty to call it Built-in Wi-Fi/)).toBeTruthy();
    const field = sheet.getByRole('textbox', { name: 'Name' });
    await user.clear(field);
    await user.click(sheet.getByRole('button', { name: 'Save' }));
    expect(device.renameRadio).toHaveBeenCalledWith(builtIn.mac, '');
  });

  it('closes without saving on Cancel', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<SystemScreen />, device, '/system');
    await user.click(screen.getByRole('button', { name: 'Rename Built-in Wi-Fi' }));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(device.renameRadio).not.toHaveBeenCalled();
  });

  it('has no pencil for a radio without a MAC address, which couldn’t be remembered', () => {
    const device = fakeDevice((router) => {
      router.status.radios[0].mac = '';
    });
    renderScreen(<SystemScreen />, device, '/system');
    const name = device.status.data!.radios[0].isUsb ? 'USB adapter 0e8d:7612' : 'Built-in Wi-Fi';
    expect(screen.queryByRole('button', { name: `Rename ${name}` })).toBeNull();
  });
});
