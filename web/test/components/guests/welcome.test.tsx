// Guests → Welcome page (your own captive portal), and its state on the Clients page.
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { GuestsScreen } from '@/screens/guests/GuestsScreen';
import { WelcomeScreen } from '@/screens/guests/WelcomeScreen';
import { fakeDevice, renderScreen } from '../render';

const PATH = '/guests/welcome';

describe('welcome page', () => {
  it('is linked from the Guests page with whether it is on', () => {
    renderScreen(<GuestsScreen />, fakeDevice(), '/guests');
    expect(screen.getByRole('link', { name: 'Welcome page' }).getAttribute('href')).toBe(PATH);
    expect(screen.getByText('Off')).toBeTruthy();
  });

  it('saves the page as filled in', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<WelcomeScreen />, device, PATH);
    await user.click(screen.getByRole('checkbox', { name: 'Show a welcome page' }));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Show it on' }), 'Both');
    const title = screen.getByRole('textbox', { name: 'Title' });
    await user.clear(title);
    await user.type(title, 'Cabin Wi-Fi');
    const message = screen.getByRole('textbox', { name: /Message/ });
    await user.clear(message);
    await user.type(message, 'Hello');
    await user.type(screen.getByRole('textbox', { name: /House rules/ }), 'Be kind.');
    fireEvent.input(screen.getByLabelText('Color'), { target: { value: '#ff3b30' } });
    await user.type(screen.getByRole('textbox', { name: /Access code/ }), '4711');
    await user.selectOptions(screen.getByRole('combobox', { name: 'Ask again' }), 'Only once');
    await user.click(screen.getByRole('checkbox', { name: /Also for wired devices/ }));
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveWelcomeSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,
        networks: 'both',
        title: 'Cabin Wi-Fi',
        message: 'Hello',
        terms: 'Be kind.',
        color: '#ff3b30',
        accessCode: '4711',
        askAgainHours: 0,
        includeWired: true,
      }),
    );
  });

  it('only offers wired devices where the hotspot has the page', () => {
    renderScreen(<WelcomeScreen />, fakeDevice(), PATH);
    // The sample's page is on the guest network only.
    expect(screen.queryByRole('checkbox', { name: /Also for wired devices/ })).toBeNull();
  });

  it('shows the router’s answer next to its field', async () => {
    const device = fakeDevice(undefined, {
      saveWelcomeSettings: async () => {
        throw new FieldErrors({ title: 'Give the page a title of up to 60 characters.' });
      },
    });
    const { user } = renderScreen(<WelcomeScreen />, device, PATH);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Give the page a title of up to 60 characters.')).toBeTruthy();
  });

  it('takes a logo and a page of your own, and removes them', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<WelcomeScreen />, device, PATH);
    const png = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'logo.png', { type: 'image/png' });
    await user.upload(screen.getByLabelText(/^Logo/), png);
    const logo = await screen.findByRole('img', { name: 'Logo' });
    expect(logo.getAttribute('src')).toMatch(/^data:image\/png;base64,/);
    await user.click(screen.getByRole('button', { name: 'Remove logo' }));
    expect(screen.queryByRole('img', { name: 'Logo' })).toBeNull();

    const page = new File(['<form method="post" action="/accept"><button>OK</button></form>'], 'page.html', {
      type: 'text/html',
    });
    await user.upload(screen.getByLabelText('HTML file'), page);
    expect(await screen.findByText('Your page')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(device.saveWelcomeSettings).toHaveBeenCalledWith(
      expect.objectContaining({ logo: '', customPage: expect.stringContaining('action="/accept"') }),
    );
  });

  it('goes back to the built-in page', async () => {
    const device = fakeDevice((router) => {
      router.welcomeSettings = { ...router.welcomeSettings, customPage: '<p>Mine</p>' };
    });
    const { user } = renderScreen(<WelcomeScreen />, device, PATH);
    expect(screen.getByText('1 KB')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Use the built-in page' }));
    expect(screen.getByLabelText('HTML file')).toBeTruthy();
  });

  it('refuses a logo and a page that are too big', async () => {
    const { user } = renderScreen(<WelcomeScreen />, fakeDevice(), PATH);
    // About 110 KB of picture: more than 100 KB.
    const big = new File([new Uint8Array(110_000)], 'logo.png', { type: 'image/png' });
    await user.upload(screen.getByLabelText(/^Logo/), big);
    expect(await screen.findByText('Use a PNG or JPEG picture of up to 100 KB.')).toBeTruthy();

    const page = new File(['x'.repeat(64_001)], 'page.html', { type: 'text/html' });
    await user.upload(screen.getByLabelText('HTML file'), page);
    expect(await screen.findByText('Use an HTML page of up to 64 KB.')).toBeTruthy();
  });

  it('says how many devices are in while it is on, and that your own devices see it too', () => {
    const device = fakeDevice((router) => {
      router.welcomeSettings = { ...router.welcomeSettings, enabled: true, networks: 'hotspot', accepted: 3 };
    });
    renderScreen(<WelcomeScreen />, device, PATH);
    expect(screen.getByText('Devices let in')).toBeTruthy();
    expect(screen.getByText('3')).toBeTruthy();
    expect(screen.getByText(/Your own devices on the hotspot see the page too/)).toBeTruthy();
  });

  it('notes when the guest network is off', () => {
    const device = fakeDevice((router) => {
      router.guestSettings = { ...router.guestSettings, enabled: false };
    });
    renderScreen(<WelcomeScreen />, device, PATH);
    expect(screen.getByText(/The guest network is off/)).toBeTruthy();
  });
});

describe('welcome page on the Clients page', () => {
  it('shows who accepted, and asks a device again', async () => {
    const device = fakeDevice((router) => {
      router.status.clients[0].welcome = 'accepted';
      router.status.clients[1].welcome = 'waiting';
    });
    const { user } = renderScreen(<ClientsScreen />, device, '/clients');
    expect(screen.getByText('Accepted')).toBeTruthy();
    expect(screen.getByText('Not accepted yet')).toBeTruthy();
    const buttons = screen.getAllByRole('button', { name: 'Ask again' });
    expect(buttons).toHaveLength(1);
    await user.click(buttons[0]);
    await waitFor(() => expect(device.resetWelcome).toHaveBeenCalledWith(device.status.data!.clients[0].mac));
  });
});
