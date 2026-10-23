// The real data layer (RealDeviceProvider): loading, deciding the phase, sending actions and
// turning the router's answers into state, against a fake router that answers like rbweb.
import { act, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { fakeRbweb, renderProvider } from './provider';

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
