// Without the internet, every feature that needs it is grayed out and says why ("Needs an
// internet connection") instead of trying and hanging. rbd refuses them too, with its own message.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen, section } from '../render';

/** A device that isn't connected to any network right now. */
const offline = (actions: Parameters<typeof fakeDevice>[1] = {}) =>
  fakeDevice((router) => {
    router.status.uplink.connected = false;
    router.status.uplink.wifi.state = 'searching';
  }, actions);

/** The button, after checking it is grayed out and gives the reason. */
function expectBlocked(button: HTMLElement) {
  expect(button).toHaveProperty('disabled', true);
  expect(within(button).getByText('Needs an internet connection')).toBeTruthy();
}

describe('without internet', () => {
  it('the speed test can’t run', () => {
    renderScreen(<InternetScreen />, offline());
    expectBlocked(within(section('Speed test')).getByRole('button', { name: /Run speed test/ }));
  });

  it('the update check can’t run', () => {
    renderScreen(<SystemScreen />, offline());
    expectBlocked(screen.getByRole('button', { name: /Check now/ }));
  });

  it('Browse plugins still shows the saved list, but can’t check again', async () => {
    const device = offline({
      pluginCatalog: async () => ({
        entries: [
          { source: 'example/plugins', file: 'x.rba', name: 'Trip Notes', version: '1.0', size: 1, runsHere: true },
        ],
        checkedAt: '2026-10-08T14:05:00Z',
      }),
    });
    renderScreen(<Pages />, device, '/plugins/catalog');
    expect(await screen.findByText('Trip Notes')).toBeTruthy();
    expectBlocked(screen.getByRole('button', { name: /Check now/ }));
  });

  it('the installed plugins still show', () => {
    renderScreen(<Pages />, offline(), '/plugins');
    expect(within(section('Installed')).getAllByRole('link').length).toBeGreaterThan(0);
    expect(screen.getByRole('link', { name: /Browse plugins/ })).toBeTruthy();
  });
});
