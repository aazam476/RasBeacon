// A plugin's own page: running it, and what it can do.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import { fakeDevice, renderScreen } from '../render';

describe('a plugin’s page', () => {
  it('stops and starts it with the Running switch', async () => {
    const device = fakeDevice();
    const plugin = device.plugins.data!.plugins.find((p) => p.id === 'adguard-home')!;
    const { user } = renderScreen(<Pages />, device, '/plugins/adguard-home');
    expect(screen.getByRole('heading', { level: 1, name: plugin.name })).toBeTruthy();
    const running = screen.getByRole('switch', { name: /Running/ });
    await user.click(running);
    expect(device.setPluginRunning).toHaveBeenCalledWith('adguard-home', plugin.state !== 'running');
  });

  it('lists what it may do', () => {
    renderScreen(<Pages />, fakeDevice(), '/plugins/adguard-home');
    const section = screen.getByRole('heading', { name: 'It can' }).closest('section')!;
    expect(within(section).getByText('Use the internet')).toBeTruthy();
  });
});
