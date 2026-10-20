// The whole app against a fake rbweb: which full-page screen or page it shows, by phase and
// address.
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { App } from '@/App';
import { fakeRbweb } from '../data/provider';

/** Opens the app at `hash` (like "#/vpn"). */
function openApp(hash = '') {
  window.location.hash = hash;
  render(<App />);
}

describe('app', () => {
  afterEach(() => {
    window.location.hash = '';
  });

  it('shows the Status page once logged in', async () => {
    fakeRbweb();
    openApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Status' })).toBeTruthy();
  });

  it('asks to log in without a session', async () => {
    fakeRbweb({ loggedIn: false });
    openApp('#/vpn');
    expect(await screen.findByRole('button', { name: 'Log in' })).toBeTruthy();
  });

  it('shows setup on a new RasBeacon', async () => {
    fakeRbweb({ answers: { 'GET /setup': { body: { mode: 'setup', claimed: false } } } });
    openApp();
    expect(await screen.findByRole('button', { name: 'Finish setup' })).toBeTruthy();
  });

  it('says when RasBeacon can’t be reached', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))),
    );
    openApp();
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('opens the hotel login page screen without logging in', async () => {
    fakeRbweb({ loggedIn: false });
    openApp('#/portal');
    expect(await screen.findByText('Nothing to do')).toBeTruthy();
  });

  it.each([
    ['#/guest', 'Guests'],
    ['#/status', 'Status'],
    ['#/nowhere', 'Status'],
  ])('takes the old or unknown address %s to %s', async (hash, page) => {
    fakeRbweb();
    openApp(hash);
    expect(await screen.findByRole('heading', { level: 1, name: page })).toBeTruthy();
  });

  it('keeps reloading the status while open', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { sent } = fakeRbweb();
    openApp();
    await screen.findByRole('heading', { level: 1, name: 'Status' });
    const before = sent.filter((r) => r.path === '/status').length;
    await vi.advanceTimersByTimeAsync(10_000);
    await waitFor(() => expect(sent.filter((r) => r.path === '/status').length).toBeGreaterThan(before));
    vi.useRealTimers();
  });
});
