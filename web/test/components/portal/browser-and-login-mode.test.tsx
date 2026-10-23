// The hotel login page screen: RasBeacon's own browser, and login mode.
import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { DeviceState } from '@/data/device';
import type { PortalStatus } from '@/data/types';
import { PortalScreen } from '@/screens/portal/PortalScreen';
import { fakeDevice, renderScreen } from '../render';

/** The login page screen with a login page in the way, changed by `portal`. */
function renderPortal(portal: Partial<PortalStatus>, browser?: DeviceState['portalBrowser']) {
  const device = fakeDevice((router) => {
    router.status.portal = {
      ...router.status.portal,
      detected: true,
      canBrowse: true,
      canStart: true,
      loginMode: false,
      browsing: false,
      secondsLeft: 0,
      ...portal,
    };
  });
  // The screen reads the portal status of its own, refreshed while it is open.
  Object.assign(device, { portal: { data: device.status.data!.portal, error: null } });
  if (browser) Object.assign(device, { portalBrowser: browser });
  return { device, ...renderScreen(<PortalScreen />, device, '/portal') };
}

describe('RasBeacon’s browser', () => {
  it('opens from the recommended way', async () => {
    const { device, user } = renderPortal({});
    await user.click(screen.getByRole('button', { name: 'Open the login page here' }));
    expect(device.openPortalBrowser).toHaveBeenCalledOnce();
  });

  /** The browser open with a picture of the page. */
  function renderBrowser() {
    return renderPortal(
      { browsing: true },
      {
        frame: {
          running: true,
          seq: 3,
          width: 800,
          height: 1200,
          url: 'http://hotel.example/login',
          title: 'Hotel Wi-Fi',
        },
        imageSrc: 'data:image/jpeg;base64,abc',
        error: null,
      },
    );
  }

  it('shows the page’s title, address and picture', () => {
    renderBrowser();
    expect(screen.getByText('Hotel Wi-Fi')).toBeTruthy();
    expect(screen.getByText('http://hotel.example/login')).toBeTruthy();
    expect(screen.getByRole('img', { name: /The network's login page/ })).toBeTruthy();
  });

  it('a press on the picture goes to the page where it started and ended', () => {
    const { device } = renderBrowser();
    const picture = screen.getByRole('img', { name: /The network's login page/ });
    fireEvent.pointerDown(picture, { clientX: 40, clientY: 60, pointerId: 1 });
    fireEvent.pointerUp(picture, { clientX: 40, clientY: 20, pointerId: 1 });
    expect(device.portalPointerDown).toHaveBeenCalledWith({ clientX: 40, clientY: 60 });
    expect(device.portalPointerUp).toHaveBeenCalledWith({ clientX: 40, clientY: 20 }, expect.anything());
  });

  it('the mouse wheel scrolls the page in the picture', () => {
    const { device } = renderBrowser();
    const picture = screen.getByRole('img', { name: /The network's login page/ });
    fireEvent.wheel(picture, { clientX: 10, clientY: 10, deltaY: 120 });
    expect(device.portalWheel).toHaveBeenCalledWith({ clientX: 10, clientY: 10 }, expect.anything(), 120);
  });

  it('sends typed text, then empties the box; an empty box sends nothing', async () => {
    const { device, user } = renderBrowser();
    const box = screen.getByLabelText('Tap a field on the page, then type here');
    await user.click(screen.getByRole('button', { name: 'Type it' }));
    expect(device.portalType).not.toHaveBeenCalled();
    await user.type(box, 'room 214{Enter}');
    expect(device.portalType).toHaveBeenCalledWith('room 214');
    expect(box).toHaveProperty('value', '');
  });

  it.each([
    ['Enter', 'Enter'],
    ['Delete', 'Backspace'],
    ['Next field', 'Tab'],
  ])('the %s key sends %s', async (label, key) => {
    const { device, user } = renderBrowser();
    await user.click(screen.getByRole('button', { name: label }));
    expect(device.portalKey).toHaveBeenCalledWith(key);
  });

  it('Back, Reload and Close go to the browser', async () => {
    const { device, user } = renderBrowser();
    await user.click(screen.getByRole('button', { name: 'Back' }));
    await user.click(screen.getByRole('button', { name: 'Reload' }));
    await user.click(screen.getByRole('button', { name: 'Close' }));
    expect(device.portalBack).toHaveBeenCalled();
    expect(device.portalReload).toHaveBeenCalled();
    expect(device.closePortalBrowser).toHaveBeenCalled();
  });

  it('shows a spinner until the first picture comes', () => {
    renderPortal({ browsing: true });
    expect(screen.getByText('Opening the login page…')).toBeTruthy();
    expect(screen.queryByRole('img', { name: /The network's login page/ })).toBeNull();
  });
});

describe('login mode', () => {
  it('starts from the other way', async () => {
    const { device, user } = renderPortal({});
    await user.click(screen.getByText('Other way: login mode'));
    await user.click(screen.getByRole('button', { name: 'Start login mode' }));
    expect(device.startLoginMode).toHaveBeenCalledOnce();
  });

  it('while on, counts down and can be extended or stopped', async () => {
    const { device, user } = renderPortal({ loginMode: true, secondsLeft: 540, loginUrl: 'http://hotel.example' });
    expect(screen.getByText('9:00 left')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open the login page' }).getAttribute('href')).toBe('http://hotel.example');
    await user.click(screen.getByRole('button', { name: '10 more minutes' }));
    await user.click(screen.getByRole('button', { name: 'Stop login mode' }));
    expect(device.extendLoginMode).toHaveBeenCalled();
    expect(device.stopLoginMode).toHaveBeenCalled();
  });
});
