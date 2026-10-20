// The notice on the Status and Internet pages while the network shows a login page, and where
// its link goes.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { PortalStatus } from '@/data/types';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen } from '../render';

function renderNotice(portal: Partial<PortalStatus>) {
  const device = fakeDevice((router) => {
    router.status.portal = {
      ...router.status.portal,
      detected: true,
      canBrowse: false,
      canStart: false,
      loginMode: false,
      browsing: false,
      secondsLeft: 0,
      ...portal,
    };
  });
  return { device, ...renderScreen(<InternetScreen />, device, '/internet') };
}

describe('login page notice', () => {
  it.each([
    { when: 'the VPN keeps devices offline', portal: { canBrowse: true }, link: 'Log in to it' },
    { when: 'login mode is on', portal: { loginMode: true, secondsLeft: 125 }, link: 'Login mode is on (2:05 left)' },
    {
      when: 'RasBeacon’s browser is open',
      portal: { browsing: true },
      link: "RasBeacon's browser is open on the login page",
    },
  ])('links to the login page screen when $when', ({ portal, link }) => {
    renderNotice(portal);
    expect(screen.getByRole('link', { name: link }).getAttribute('href')).toBe('/portal');
  });

  it('opens the page itself when devices can reach it', () => {
    renderNotice({ loginUrl: 'http://hotel.example/login' });
    const link = screen.getByRole('link', { name: 'Open the login page' });
    expect(link.getAttribute('href')).toBe('http://hotel.example/login');
    expect(screen.getByText('Open it to log in; your devices can reach it directly.')).toBeTruthy();
  });

  it('Check again asks the router', async () => {
    const { device, user } = renderNotice({});
    await user.click(screen.getByRole('button', { name: 'Check again' }));
    expect(device.checkPortal).toHaveBeenCalledOnce();
  });
});
