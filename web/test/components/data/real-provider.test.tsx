// The real data layer (RealDeviceProvider): loading, deciding the phase, sending actions and
// turning the router's answers into state, against a fake router that answers like rbweb.
import { act, render, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { createMockRouter } from '@/data/demo/mockData';
import { useDevice, type Device } from '@/data/device';
import { FieldErrors } from '@/data/errors';
import { RealDeviceProvider } from '@/data/real/RealDeviceProvider';

interface Sent {
  method: string;
  path: string;
  body: unknown;
}

/**
 * A fake rbweb: GETs answer with the demo's sample data, other requests with `answers[route]`
 * (route like "POST /system/hostname"; status 200 and `{}` if missing). Records every request.
 */
function fakeRbweb(
  options: { loggedIn?: boolean; answers?: Record<string, { status?: number; body?: unknown }> } = {},
) {
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
  };
  const sent: Sent[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const method = init.method ?? 'GET';
      const path = url.replace('/api/v1', '');
      sent.push({ method, path, body: typeof init.body === 'string' ? JSON.parse(init.body) : undefined });
      const answer = options.answers?.[`${method} ${path}`];
      const status = answer?.status ?? 200;
      const body = answer?.body ?? (method === 'GET' ? gets[path] : {}) ?? {};
      return new Response(status === 204 ? null : JSON.stringify(body), { status });
    }),
  );
  return { router, sent };
}

/** Renders the provider and returns a handle on the device it gives the screens. */
function renderProvider() {
  const current: { device?: Device } = {};
  function Probe() {
    const device = useDevice();
    useEffect(() => {
      current.device = device;
    });
    return null;
  }
  render(
    <MemoryRouter>
      <RealDeviceProvider>
        <Probe />
      </RealDeviceProvider>
    </MemoryRouter>,
  );
  return () => current.device!;
}

describe('starting up', () => {
  it('loads the setup state and the session, then every setting', async () => {
    const { sent } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase).toEqual({ kind: 'ready' }));
    await waitFor(() => expect(device().accessSettings.data).toBeDefined());
    const loaded = sent.filter((r) => r.method === 'GET').map((r) => r.path);
    expect(loaded).toEqual(
      expect.arrayContaining(['/setup', '/session', '/status', '/settings/access', '/settings/updates']),
    );
  });

  it('asks to log in without a session', async () => {
    fakeRbweb({ loggedIn: false });
    const device = renderProvider();
    await waitFor(() => expect(device().phase).toEqual({ kind: 'login' }));
  });

  it('says RasBeacon can’t be reached when nothing answers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))),
    );
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('unreachable'));
  });
});

describe('actions', () => {
  it('stores the saved allow list from the router’s answer', async () => {
    const saved = { allowListOn: true, allowed: ['3a:1f:22:aa:bb:cc'], maxDevices: 8 };
    fakeRbweb({ answers: { 'PUT /settings/access': { body: saved } } });
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    await act(() => device().saveAccessSettings(saved));
    expect(device().accessSettings.data).toEqual(saved);
  });

  it('puts the router’s error next to the right field', async () => {
    fakeRbweb({
      answers: {
        'POST /settings/vpn/profile/rename': {
          status: 400,
          body: { error: 'Another profile already has this name.', field: 'newName' },
        },
      },
    });
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    const error = await device()
      .renameVpnProfile('wireguard.conf', 'Home')
      .catch((e: unknown) => e);
    // The rename sheet's field is "name"; the router calls it "newName".
    expect(error).toBeInstanceOf(FieldErrors);
    expect((error as FieldErrors).fields).toEqual({ name: 'Another profile already has this name.' });
  });

  it('downloads the backup from the link the router answers with', async () => {
    const { sent } = fakeRbweb({
      answers: { 'POST /system/backup': { body: { token: 'abc', fileName: 'rasbeacon-backup.rbbackup' } } },
    });
    const clicked: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked.push(`${this.getAttribute('href')} ${this.download}`);
    });
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    await act(() => device().backUp('backup-pass-42', 'everything'));
    expect(sent.find((r) => r.path === '/system/backup')?.body).toEqual({
      password: 'backup-pass-42',
      plugins: 'everything',
    });
    expect(clicked).toEqual(['/api/v1/system/backup/abc rasbeacon-backup.rbbackup']);
  });

  it('shows "RasBeacon is off" after shutting down, until Try again', async () => {
    fakeRbweb({ answers: { 'POST /system/shutdown': { status: 204 } } });
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    await act(() => device().shutDown());
    expect(device().phase).toEqual({ kind: 'notice', notice: 'off' });
    await act(() => device().retry());
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
  });
});
