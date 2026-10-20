// Shared by the component tests: a fake device for screens to read, and a way to render a
// screen the way the app does (router, error toasts, device).
//
// A test usually reads like this:
//
//   const device = fakeDevice((router) => { router.vpnSettings.profiles = []; });
//   renderScreen(<VpnScreen />, device);
//   await user.click(screen.getByRole('button', { name: 'Save' }));
//   expect(device.saveVpnDetails).toHaveBeenCalledWith(...);

import { createEvent, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router';
import { vi, type Mock } from 'vitest';
import { createMockRouter, type MockRouter } from '@/data/demo/mockData';
import { DeviceContext, type Device, type DeviceActions, type DeviceState } from '@/data/device';
import { ErrorToastProvider } from '@/ui';

/** A device whose actions are all Vitest spies, so tests can check what a button called. */
export type FakeDevice = Device & { [K in keyof DeviceActions]: Mock<DeviceActions[K]> };

/**
 * The demo's sample router (web/src/data/demo/mockData.ts), changed by `change` if given, as a
 * logged-in device. Every action is a spy that resolves with nothing; `actions` replaces some,
 * for example to make one fail or answer something.
 */
export function fakeDevice(change?: (router: MockRouter) => void, actions: Partial<DeviceActions> = {}): FakeDevice {
  const router = createMockRouter();
  change?.(router);
  const loaded = <T,>(data: T) => ({ data, error: null });
  const state: DeviceState = {
    phase: { kind: 'ready' },
    setup: router.setup,
    // As in the demo, the status lists the installed plugins.
    status: loaded({
      ...router.status,
      plugins: router.plugins.plugins.map(({ id, name, state, icon }) => ({ id, name, state, icon })),
    }),
    hotspotSettings: loaded(router.hotspotSettings),
    guestSettings: loaded(router.guestSettings),
    uplinkSettings: loaded(router.uplinkSettings),
    vpnSettings: loaded(router.vpnSettings),
    sshSettings: loaded(router.sshSettings),
    dnsSettings: loaded(router.dnsSettings),
    swapSettings: loaded(router.swapSettings),
    updateSettings: loaded(router.updateSettings),
    accessSettings: loaded(router.accessSettings),
    usageSettings: loaded(router.usageSettings),
    plugins: loaded(router.plugins),
    httpsSettings: loaded(router.httpsSettings),
    nearbyNetworks: router.nearbyNetworks,
    portal: loaded(router.status.portal),
    portalBrowser: { frame: undefined, imageSrc: '', error: null },
    diagnosticsUrl: '/api/v1/diagnostics',
  };
  // Any action a screen asks for becomes a spy the first time, and stays the same spy.
  const spies = new Map<string | symbol, Mock>();
  return new Proxy(state as FakeDevice, {
    get(target, key) {
      if (key in target) return target[key as keyof DeviceState];
      if (!spies.has(key)) {
        const given = actions[key as keyof DeviceActions] as ((...args: unknown[]) => unknown) | undefined;
        spies.set(key, vi.fn(given ?? (async () => undefined)));
      }
      return spies.get(key);
    },
  });
}

/**
 * Renders a screen inside what the app wraps every screen in, at `path` (the screen's own
 * address, for its links). Returns Testing Library's helpers and `user`, which types and clicks
 * like a person; `rerender` draws the screen again after a test changed the device.
 */
export function renderScreen(screen: ReactNode, device: FakeDevice, path = '/') {
  const user = userEvent.setup();
  const wrap = (ui: ReactNode) => (
    <MemoryRouter initialEntries={[path]}>
      <ErrorToastProvider>
        <DeviceContext value={device}>{ui}</DeviceContext>
      </ErrorToastProvider>
    </MemoryRouter>
  );
  const result = render(wrap(screen));
  // Drawn again inside the same wrappers, after a test changed the device.
  return { ...result, user, rerender: (ui: ReactNode) => result.rerender(wrap(ui)) };
}

/**
 * Opens a collapsed row (Disclosure, a native <details>) by its title and returns it, to look
 * inside with `within(…)`. jsdom keeps a closed row's content in the page, so a test should look
 * only inside the row it opened (browsers hide it).
 */
export async function openRow(user: ReturnType<typeof userEvent.setup>, title: string): Promise<HTMLElement> {
  // Its title, in the row itself (the same words may also label a field inside).
  const summary = screen.getAllByText(title, { exact: true }).find((element) => element.closest('summary'));
  const details = summary?.closest('details');
  if (!summary || !details) throw new Error(`"${title}" is not a row that opens`);
  await user.click(summary);
  return details;
}

/**
 * The group of rows under a heading (a Section), to look inside with `within(…)`. Sections
 * aren't labelled regions (see web/src/ui/Section.tsx), so this finds them by their heading.
 */
export function section(title: string): HTMLElement {
  const group = screen.getByRole('heading', { name: title }).closest('section');
  if (!group) throw new Error(`"${title}" is not a section's heading`);
  return group;
}

type PointerKind = 'pointerDown' | 'pointerMove' | 'pointerUp' | 'pointerCancel';

/**
 * A finger's pointer event at `clientY`, `time` ms into the gesture, for swipes and pulls whose
 * speed matters (Testing Library can't give an event a time of its own).
 */
export function finger(target: Element, kind: PointerKind, clientY: number, time: number, init: object = {}) {
  const event = createEvent[kind](target, { pointerId: 1, pointerType: 'touch', isPrimary: true, clientY, ...init });
  // From 1 s on: React reads a time of 0 as "none" and uses the clock instead.
  Object.defineProperty(event, 'timeStamp', { value: 1000 + time });
  fireEvent(target, event);
}
