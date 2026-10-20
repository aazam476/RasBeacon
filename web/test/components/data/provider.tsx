// Shared by the data layer tests: a fake rbweb, and the real provider rendered with a handle on
// the device it gives the screens.
import { render } from '@testing-library/react';
import { useEffect } from 'react';
import { MemoryRouter } from 'react-router';
import { vi } from 'vitest';
import { createMockRouter } from '@/data/demo/mockData';
import { useDevice, type Device } from '@/data/device';
import { RealDeviceProvider } from '@/data/real/RealDeviceProvider';

/** How the fake answers one route: `status` (200 if left out) and `body`, or never (`hang`). */
export interface FakeAnswer {
  status?: number;
  body?: unknown;
  hang?: boolean;
}

export interface Sent {
  method: string;
  path: string;
  body: unknown;
}

/**
 * A fake rbweb: GETs answer with the demo's sample data, other requests with `answers[route]`
 * (route like "POST /system/hostname"; status 200 and `{}` if missing). Records every request,
 * with an upload's body as `'<file>'`. `loaded` are the paths it answers with sample data;
 * `answers` can be changed during a test (RasBeacon going away and coming back).
 */
export function fakeRbweb(options: { loggedIn?: boolean; answers?: Record<string, FakeAnswer> } = {}) {
  const answers = (options.answers ??= {});
  const router = createMockRouter();
  const gets: Record<string, unknown> = {
    '/setup': router.setup,
    '/session': { loggedIn: options.loggedIn ?? true, csrfToken: 'token' },
    '/status': router.status,
    '/settings/hotspot': router.hotspotSettings,
    '/settings/guest': router.guestSettings,
    '/settings/uplink': router.uplinkSettings,
    '/settings/vpn': router.vpnSettings,
    '/settings/ssh': router.sshSettings,
    '/settings/dns': router.dnsSettings,
    '/settings/swap': router.swapSettings,
    '/settings/updates': router.updateSettings,
    '/settings/access': router.accessSettings,
    '/settings/usage': router.usageSettings,
    '/settings/https': router.httpsSettings,
    '/plugins': router.plugins,
    '/portal': router.status.portal,
  };
  const sent: Sent[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const method = init.method ?? 'GET';
      const path = url.replace('/api/v1', '');
      sent.push({
        method,
        path,
        body: typeof init.body === 'string' ? JSON.parse(init.body) : init.body === undefined ? undefined : '<file>',
      });
      const answer = answers[`${method} ${path}`];
      if (answer?.hang) return new Promise<Response>(() => undefined);
      const status = answer?.status ?? 200;
      const body = answer?.body ?? (method === 'GET' ? gets[path] : {}) ?? {};
      return new Response(status === 204 ? null : JSON.stringify(body), { status });
    }),
  );
  return { router, sent, answers, loaded: new Set(Object.keys(gets)) };
}

/** Renders the provider and returns a handle on the device it gives the screens. */
export function renderProvider(path = '/') {
  const current: { device?: Device } = {};
  function Probe() {
    const device = useDevice();
    useEffect(() => {
      current.device = device;
    });
    return null;
  }
  render(
    <MemoryRouter initialEntries={[path]}>
      <RealDeviceProvider>
        <Probe />
      </RealDeviceProvider>
    </MemoryRouter>,
  );
  return () => current.device!;
}
