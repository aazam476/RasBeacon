// How every request is sent: the login's CSRF token, the router's errors, and no answer at all.
import { describe, expect, it, vi } from 'vitest';
import * as api from '@/data/real/api';
import { fakeRouter } from './helpers';

describe('sending requests', () => {
  it('sends the CSRF token from the login with every change, not with reads', async () => {
    fakeRouter({ body: { loggedIn: true, csrfToken: 'token-1' } });
    await api.logIn('tangerine-orbit-42', false);

    const sent = fakeRouter();
    await api.reboot();
    await api.getStatus();
    expect(sent[0].headers['X-CSRF-Token']).toBe('token-1');
    expect(sent[1].headers['X-CSRF-Token']).toBeUndefined();

    await api.logOut();
    const afterLogout = fakeRouter();
    await api.reboot();
    expect(afterLogout[0].headers['X-CSRF-Token']).toBeUndefined();
  });

  it('claiming sends the setup, then reads the session for the token', async () => {
    const sent = fakeRouter({ body: { loggedIn: true, csrfToken: 'token-2' } });
    const claim = {
      adminPassword: 'tangerine-orbit-42',
      hotspotSsid: 'Travel',
      hotspotPassword: 'kq7m-2xvd',
      hotspotBand: '5GHz',
    };
    await api.claim(claim);
    expect(sent.map((r) => `${r.method} ${r.path}`)).toEqual(['POST /setup/claim', 'GET /session']);
    expect(sent[0].body).toEqual(claim);
  });

  it('turns the router’s error into a message for the right field', async () => {
    fakeRouter({ status: 400, body: { error: 'Use only lowercase letters, digits and hyphens.', field: 'hostname' } });
    const error = await api.saveHostname('my pi').catch((e: unknown) => e);
    expect(error).toBeInstanceOf(api.ApiError);
    expect(error).toMatchObject({
      status: 400,
      field: 'hostname',
      message: 'Use only lowercase letters, digits and hyphens.',
    });
  });

  it('gives a general message when the error isn’t JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('Bad gateway', { status: 502 })),
    );
    await expect(api.getStatus()).rejects.toMatchObject({
      status: 502,
      message: 'RasBeacon answered with an error (502).',
    });
  });

  it('says RasBeacon can’t be reached when nothing answers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))),
    );
    await expect(api.getStatus()).rejects.toMatchObject({ status: 0, message: api.UNREACHABLE });
  });

  it('answers undefined for “204 No Content”', async () => {
    fakeRouter({ status: 204 });
    await expect(api.shutDown()).resolves.toBeUndefined();
  });

  it('links to a backup by its token, encoded', () => {
    expect(api.backupDownloadUrl('a/b c')).toBe('/api/v1/system/backup/a%2Fb%20c');
  });
});
