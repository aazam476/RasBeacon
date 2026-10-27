// Every piece of data a page loads: when it can't be loaded, the page says why and Try again
// loads that piece again. When a later refresh fails, the page keeps what it showed.
import { screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it } from 'vitest';
import type { DataKey } from '@/data/device';
import { ClientsScreen } from '@/screens/clients/ClientsScreen';
import { GuestsScreen } from '@/screens/guests/GuestsScreen';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { HttpsScreen } from '@/screens/https/HttpsScreen';
import { WelcomeScreen } from '@/screens/guests/WelcomeScreen';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { PortForwardingScreen } from '@/screens/internet/PortForwardingScreen';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { PortalScreen } from '@/screens/portal/PortalScreen';
import { SshScreen } from '@/screens/ssh/SshScreen';
import { StatusScreen } from '@/screens/status/StatusScreen';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { VpnScreen } from '@/screens/vpn/VpnScreen';
import { fakeDevice, renderScreen } from '../render';

/** Each page with the data it loads. */
const cases: { key: DataKey; page: string; screen: ReactNode }[] = [
  { key: 'status', page: 'Status', screen: <StatusScreen /> },
  { key: 'uplinkSettings', page: 'Internet', screen: <InternetScreen /> },
  { key: 'dnsSettings', page: 'Internet', screen: <InternetScreen /> },
  { key: 'vpnSettings', page: 'VPN', screen: <VpnScreen /> },
  { key: 'hotspotSettings', page: 'Hotspot', screen: <HotspotScreen /> },
  { key: 'accessSettings', page: 'Hotspot', screen: <HotspotScreen /> },
  { key: 'guestSettings', page: 'Guests', screen: <GuestsScreen /> },
  { key: 'usageSettings', page: 'Clients', screen: <ClientsScreen /> },
  { key: 'sshSettings', page: 'SSH', screen: <SshScreen /> },
  { key: 'httpsSettings', page: 'HTTPS', screen: <HttpsScreen /> },
  { key: 'swapSettings', page: 'System', screen: <SystemScreen /> },
  { key: 'updateSettings', page: 'System', screen: <SystemScreen /> },
  { key: 'plugins', page: 'Plugins', screen: <PluginsScreen /> },
  { key: 'portForwarding', page: 'Port forwarding', screen: <PortForwardingScreen /> },
  { key: 'welcomeSettings', page: 'Welcome page', screen: <WelcomeScreen /> },
  { key: 'portal', page: 'the login page screen', screen: <PortalScreen /> },
];

describe('Try again', () => {
  it.each(cases)('$page loads $key again', async ({ key, screen: page }) => {
    const device = fakeDevice();
    Object.assign(device, { [key]: { data: undefined, error: 'RasBeacon did not answer.' } });
    const { user } = renderScreen(page, device);
    const notice = screen.getByText('RasBeacon did not answer.').closest('[role=alert]') as HTMLElement;
    await user.click(within(notice).getByRole('button', { name: 'Try again' }));
    if (key === 'status') expect(device.refreshStatus).toHaveBeenCalled();
    else expect(device.reload).toHaveBeenCalledWith(key);
  });

  it('a failed refresh keeps what the page shows, and says so', async () => {
    const device = fakeDevice();
    Object.assign(device, { vpnSettings: { data: device.vpnSettings.data, error: 'RasBeacon did not answer.' } });
    const { user } = renderScreen(<VpnScreen />, device);
    expect(screen.getByText('Last refresh failed: RasBeacon did not answer.')).toBeTruthy();
    expect(screen.getByRole('switch', { name: /Kill switch/ })).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(device.reload).toHaveBeenCalledWith('vpnSettings');
  });
});
