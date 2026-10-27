// Internet page: the MAC address each uplink shows its networks (the Pi's own, random or cloned).
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen } from '../render';

/** The MAC address form (the page has other Save buttons). */
const macForm = () => screen.getByRole('heading', { name: 'MAC address' }).closest('form')!;

describe('MAC address', () => {
  it('starts with the Pi’s own addresses and no reconnect note', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    const form = within(macForm());
    expect((form.getByLabelText('Wi-Fi') as HTMLSelectElement).value).toBe('');
    expect((form.getByLabelText('Ethernet') as HTMLSelectElement).value).toBe('');
    expect(screen.queryByText(/Saving reconnects/)).toBeNull();
  });

  it('is hidden when there is neither a Wi-Fi uplink nor an Ethernet port', () => {
    const device = fakeDevice((router) => {
      router.uplinkSettings.wifiUplinkAvailable = false;
      router.uplinkSettings.ethernetPresent = false;
    });
    renderScreen(<InternetScreen />, device, '/internet');
    expect(screen.queryByRole('heading', { name: 'MAC address' })).toBeNull();
  });

  it('shows the random address in use', () => {
    const device = fakeDevice((router) => {
      router.uplinkSettings.ethernetMac = { mode: 'random', address: '06:11:22:33:44:55' };
    });
    renderScreen(<InternetScreen />, device, '/internet');
    expect(within(macForm()).getByText('06:11:22:33:44:55')).toBeTruthy();
    expect(screen.getByText(/Saving reconnects/)).toBeTruthy();
  });

  it('clones a device’s address and warns while that device is on the hotspot', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const form = within(macForm());
    await user.selectOptions(form.getByLabelText('Wi-Fi'), 'cloned');
    await user.selectOptions(form.getByLabelText('Copy a device’s address'), 'f4:d4:88:62:3b:01');
    expect((form.getByLabelText('Address to use') as HTMLInputElement).value).toBe('f4:d4:88:62:3b:01');
    expect(form.getByText(/Work laptop on your hotspot uses this address/)).toBeTruthy();
    await user.click(form.getByRole('button', { name: 'Save' }));
    expect(device.saveUplinkMacs).toHaveBeenCalledWith({ mode: 'cloned', address: 'f4:d4:88:62:3b:01' }, { mode: '' });
    expect(await screen.findByText('Saved.')).toBeTruthy();
  });

  it('keeps the random address it has, and sends a typed one in lower case', async () => {
    const device = fakeDevice((router) => {
      router.uplinkSettings.wifiMac = { mode: 'random', address: '06:11:22:33:44:55' };
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const form = within(macForm());
    await user.selectOptions(form.getByLabelText('Ethernet'), 'cloned');
    await user.type(form.getByLabelText('Address to use'), ' 3A:1F:22:AA:BB:CC ');
    await user.click(form.getByRole('button', { name: 'Save' }));
    expect(device.saveUplinkMacs).toHaveBeenCalledWith(
      { mode: 'random', address: '06:11:22:33:44:55' },
      { mode: 'cloned', address: '3a:1f:22:aa:bb:cc' },
    );
  });

  it('asks for a new random address when switching to random', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const form = within(macForm());
    await user.selectOptions(form.getByLabelText('Wi-Fi'), 'random');
    await user.click(form.getByRole('button', { name: 'Save' }));
    expect(device.saveUplinkMacs).toHaveBeenCalledWith({ mode: 'random', address: undefined }, { mode: '' });
  });

  it('shows the router’s answer next to the address', async () => {
    const device = fakeDevice(undefined, {
      saveUplinkMacs: async () => {
        throw new FieldErrors({ wifiMac: 'Type a MAC address like 3a:1f:22:aa:bb:cc.', ethernetMac: 'Not this one.' });
      },
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const form = within(macForm());
    await user.selectOptions(form.getByLabelText('Wi-Fi'), 'cloned');
    await user.type(form.getByLabelText('Address to use'), '01:00:5e:00:00:01');
    await user.click(form.getByRole('button', { name: 'Save' }));
    expect(await form.findByText('Type a MAC address like 3a:1f:22:aa:bb:cc.')).toBeTruthy();
    expect(form.getByText('Not this one.')).toBeTruthy();
    expect(screen.queryByText('Saved.')).toBeNull();
  });
});
