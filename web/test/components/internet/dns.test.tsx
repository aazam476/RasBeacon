// Internet page: the DNS servers.
import { within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, openRow, renderScreen } from '../render';

describe('DNS servers', () => {
  it('keeps the list out of the row until it is opened', () => {
    const { container } = renderScreen(
      <InternetScreen />,
      fakeDevice((router) => {
        router.dnsSettings = { servers: ['1.1.1.1'], withVpn: false };
      }),
      '/internet',
    );
    const summary = [...container.querySelectorAll('summary')].find((row) => row.textContent?.includes('DNS servers'))!;
    expect(summary.textContent).not.toContain('1.1.1.1');
  });

  it('saves the servers in the order typed', async () => {
    const device = fakeDevice((router) => {
      router.dnsSettings = { servers: [], withVpn: false };
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const row = within(await openRow(user, 'DNS servers'));
    await user.type(row.getByLabelText(/^DNS servers/), 'https://cloudflare-dns.com/dns-query\n1.1.1.1');
    await user.click(row.getByLabelText('Use these servers while the VPN is on'));
    await user.click(row.getByRole('button', { name: 'Save' }));
    expect(device.saveDnsSettings).toHaveBeenCalledWith('https://cloudflare-dns.com/dns-query\n1.1.1.1', true);
  });

  it('can’t use the servers with the VPN while there are none', async () => {
    const device = fakeDevice((router) => {
      router.dnsSettings = { servers: [], withVpn: false };
    });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const row = within(await openRow(user, 'DNS servers'));
    expect(row.getByLabelText('Use these servers while the VPN is on')).toHaveProperty('disabled', true);
  });
});
