// The speed test on the Internet page: one at a time, and the button says a test is running for
// as long as it runs, also after leaving the page and coming back.
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';
import { App } from '@/App';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeRbweb } from '../data/provider';
import { fakeDevice, renderScreen, section } from '../render';

describe('speed test', () => {
  afterEach(() => {
    window.location.hash = '';
  });

  it('still says Running… after going to another page and back', async () => {
    // The test takes a while: the router hasn't answered yet, and its status says it runs.
    const { answers, router } = fakeRbweb({ answers: { 'POST /speedtest': { hang: true } } });
    window.location.hash = '#/internet';
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole('button', { name: 'Run speed test' }));
    expect(await screen.findByRole('button', { name: 'Running…' })).toBeTruthy();
    answers['GET /status'] = { body: { ...router.status, speedtest: { ...router.status.speedtest, running: true } } };

    await user.click(screen.getByRole('link', { name: 'VPN' }));
    await screen.findByRole('heading', { level: 1, name: 'VPN' });
    await user.click(screen.getByRole('link', { name: 'Internet' }));
    await screen.findByRole('heading', { level: 1, name: 'Internet' });
    expect(within(section('Speed test')).getByRole('button', { name: 'Running…' })).toBeTruthy();
    expect(within(section('Speed test')).queryByRole('button', { name: 'Run speed test' })).toBeNull();
  });

  it('shows the result, and the button again, when the test ends', async () => {
    const { answers } = fakeRbweb();
    answers['POST /speedtest'] = {
      body: {
        provider: 'Cloudflare',
        running: false,
        last: { server: 'Frankfurt (FRA)', ageSeconds: 0, downloadMbps: 312, uploadMbps: 98, pingMs: 9 },
      },
    };
    window.location.hash = '#/internet';
    const user = userEvent.setup();
    render(<App />);
    await user.click(await screen.findByRole('button', { name: 'Run speed test' }));
    await waitFor(() => expect(within(section('Speed test')).getByText('Frankfurt (FRA)')).toBeTruthy());
    expect(within(section('Speed test')).getByRole('button', { name: 'Run speed test' })).toBeTruthy();
  });

  it('a test started elsewhere (another device) also shows as running', async () => {
    const device = fakeDevice((router) => {
      router.status.speedtest.running = true;
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const button = within(section('Speed test')).getByRole('button', { name: 'Running…' });
    await user.click(button);
    expect(device.runSpeedtest).not.toHaveBeenCalled();
  });

  it('runs when asked', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(within(section('Speed test')).getByRole('button', { name: 'Run speed test' }));
    expect(device.runSpeedtest).toHaveBeenCalledOnce();
  });

  it('runs at once on an ordinary connection, without asking', async () => {
    const device = fakeDevice((router) => {
      router.status.hotspot.metered = false;
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(within(section('Speed test')).getByRole('button', { name: 'Run speed test' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(device.runSpeedtest).toHaveBeenCalledOnce();
  });

  it('asks first on a metered connection, since a test uses a lot of data', async () => {
    const device = fakeDevice((router) => {
      router.status.hotspot.metered = true;
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(within(section('Speed test')).getByRole('button', { name: 'Run speed test' }));
    const dialog = within(screen.getByRole('dialog', { name: 'Run a speed test?' }));
    expect(dialog.getByText(/several hundred MB of data/)).toBeTruthy();
    await user.click(dialog.getByRole('button', { name: 'Cancel' }));
    expect(device.runSpeedtest).not.toHaveBeenCalled();

    await user.click(within(section('Speed test')).getByRole('button', { name: 'Run speed test' }));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Run a speed test?' })).getByRole('button', { name: 'Run test' }),
    );
    expect(device.runSpeedtest).toHaveBeenCalledOnce();
  });
});
