// What the data layer waits for: RasBeacon coming back after a restart or a factory reset, and
// the pages that keep their data fresh. The waits take minutes, so the tests use a fake clock.
import { act, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fakeRbweb, renderProvider } from './provider';

// Actions that wait for minutes run outside act(): an act() left open would hold back the next
// test's rendering. The clock moves inside act(), so React applies what happened.

/** Moves the fake clock on, letting the provider's requests and answers happen on the way. */
async function wait(ms: number) {
  await act(() => vi.advanceTimersByTimeAsync(ms));
}

const count = (sent: { method: string; path: string }[], route: string) =>
  sent.filter((r) => `${r.method} ${r.path}` === route).length;

// React schedules its work with setImmediate, kept real: it keeps the first one it finds.
beforeEach(() =>
  vi.useFakeTimers({
    shouldAdvanceTime: true,
    toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'],
  }),
);
afterEach(() => vi.useRealTimers());

describe('rebooting', () => {
  it('waits until RasBeacon went away and answers again, then reloads the session', async () => {
    const { sent, answers } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));

    let done = false;
    void device()
      .reboot()
      .then(() => (done = true));
    await wait(5_000);
    // Restarting: nothing answers for a while.
    answers['GET /setup'] = { status: 502 };
    await wait(20_000);
    expect(done).toBe(false);
    delete answers['GET /setup'];
    await wait(5_000);
    await waitFor(() => expect(done).toBe(true));
    expect(count(sent, 'POST /system/reboot')).toBe(1);
    expect(count(sent, 'GET /session')).toBeGreaterThan(1);
  });

  it('gives a restart that never seemed to go away a minute and a half', async () => {
    fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    let done = false;
    void device()
      .reboot()
      .then(() => (done = true));
    await wait(60_000);
    expect(done).toBe(false);
    await wait(40_000);
    await waitFor(() => expect(done).toBe(true));
  });

  it('says so when RasBeacon doesn’t come back within five minutes', async () => {
    const { answers } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    answers['GET /setup'] = { status: 502 };
    let error: unknown;
    void device()
      .reboot()
      .catch((e: unknown) => (error = e));
    await wait(6 * 60_000);
    await waitFor(() => expect(String(error)).toContain("RasBeacon didn't answer after restarting"));
  });

  it('restarting into an update waits for the restart the same way', async () => {
    const { sent, answers } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    let done = false;
    void device()
      .restartIntoUpdate()
      .then(() => (done = true));
    await wait(5_000);
    answers['GET /setup'] = { status: 502 };
    await wait(20_000);
    expect(done).toBe(false);
    delete answers['GET /setup'];
    await wait(5_000);
    await waitFor(() => expect(done).toBe(true));
    expect(count(sent, 'POST /system/update/restart')).toBe(1);
  });

  it('renaming waits for the restart the same way', async () => {
    const { sent } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().status.data).toBeDefined());
    let done = false;
    void device()
      .saveHostname('  Travel-Pi ')
      .then(() => (done = true));
    await wait(100_000);
    await waitFor(() => expect(done).toBe(true));
    // Sent as RasBeacon stores it.
    expect(sent.find((r) => r.path === '/system/hostname')?.body).toEqual({ hostname: 'travel-pi' });
  });
});

describe('radio roles', () => {
  it('changing them and going back wait for the restart the same way', async () => {
    const { sent } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().status.data).toBeDefined());
    let done = 0;
    void device()
      .saveRadioRoles({ '00:0e:8e:00:00:03': 'guest' })
      .then(() => done++);
    await wait(100_000);
    await waitFor(() => expect(done).toBe(1));
    expect(sent.find((r) => r.path === '/settings/radios')?.body).toEqual({ roles: { '00:0e:8e:00:00:03': 'guest' } });
    void device()
      .revertRadioRoles()
      .then(() => done++);
    await wait(100_000);
    await waitFor(() => expect(done).toBe(2));
    expect(count(sent, 'POST /settings/radios/revert')).toBe(1);
  });
});

describe('factory reset', () => {
  it('forgets everything, waits for the setup network, then shows setup', async () => {
    const { answers } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().status.data).toBeDefined());

    await act(() => device().factoryReset('tangerine-orbit-42'));
    expect(device().status.data).toBeUndefined();
    expect(device().phase.kind).toBe('loading');

    // Still the old RasBeacon for a moment, then gone, then in setup.
    await wait(3_000);
    answers['GET /setup'] = { status: 502 };
    await wait(6_000);
    expect(device().phase.kind).toBe('loading');
    answers['GET /setup'] = { body: { mode: 'setup', claimed: false } };
    await wait(3_000);
    await waitFor(() => expect(device().phase.kind).toBe('setup'));
  });

  it('says RasBeacon can’t be reached when it never comes back', async () => {
    const { answers } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    await act(() => device().factoryReset('tangerine-orbit-42'));
    answers['GET /setup'] = { status: 502 };
    await wait(100 * 3_000);
    await waitFor(() => expect(device().phase.kind).toBe('unreachable'));
  });
});

describe('keeping data fresh', () => {
  it('the plugins’ pages reload the plugins every few seconds', async () => {
    const { sent } = fakeRbweb();
    renderProvider('/plugins');
    await wait(1_000);
    const before = count(sent, 'GET /plugins');
    await wait(8_000);
    expect(count(sent, 'GET /plugins') - before).toBeGreaterThanOrEqual(2);
  });

  it('the login page screen reloads the portal status every 5 seconds', async () => {
    const { sent } = fakeRbweb();
    renderProvider('/portal');
    await wait(1_000);
    const before = count(sent, 'GET /portal');
    await wait(10_000);
    expect(count(sent, 'GET /portal') - before).toBeGreaterThanOrEqual(2);
  });

  it('Check again looks at the portal status once more after the router’s check', async () => {
    const { sent } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    await act(() => device().checkPortal());
    const before = count(sent, 'GET /portal');
    await wait(6_000);
    expect(count(sent, 'GET /portal')).toBe(before + 1);
  });

  it('a page shows the problem when its data can’t be loaded, and keeps the old data', async () => {
    const { answers } = fakeRbweb();
    const device = renderProvider();
    await waitFor(() => expect(device().status.data).toBeDefined());
    answers['GET /status'] = { status: 500, body: { error: 'rbd is restarting.' } };
    await wait(10_000);
    await waitFor(() => expect(device().status.error).toBe('rbd is restarting.'));
    expect(device().status.data).toBeDefined();
  });

  it('RasBeacon’s browser tries again after a failed picture', async () => {
    const { sent, answers, router } = fakeRbweb({});
    answers['GET /portal'] = { body: { ...router.status.portal, browsing: true } };
    answers['GET /portal/browser/frame?after=0'] = { status: 500, body: { error: 'The browser is busy.' } };
    const device = renderProvider('/portal');
    await waitFor(() => expect(device().portalBrowser.error).toBe('The browser is busy.'));
    const tries = count(sent, 'GET /portal/browser/frame?after=0');
    await wait(2_000);
    expect(count(sent, 'GET /portal/browser/frame?after=0')).toBeGreaterThan(tries);
  });

  it('the browser closing by itself (the login worked) closes it here too', async () => {
    const { answers, router } = fakeRbweb();
    answers['GET /portal'] = { body: { ...router.status.portal, browsing: true } };
    answers['GET /portal/browser/frame?after=0'] = { body: { running: false } };
    const device = renderProvider('/portal');
    await wait(1_000);
    await waitFor(() => expect(device().portalBrowser).toEqual({ frame: undefined, imageSrc: '', error: null }));
  });
});
