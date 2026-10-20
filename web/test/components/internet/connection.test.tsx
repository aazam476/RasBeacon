// Internet page: the connection in use, or why there is none, and the Ethernet port.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen, section } from '../render';

/** The Internet page, with the uplink changed by `change`. */
function renderInternet(change?: (uplink: MockRouter['status']['uplink'], router: MockRouter) => void) {
  const device = fakeDevice((router) => change?.(router.status.uplink, router));
  return { device, ...renderScreen(<InternetScreen />, device, '/internet') };
}

describe('connection', () => {
  it('shows the Wi-Fi network in use and its details', () => {
    renderInternet();
    const connection = within(section('Connection'));
    expect(connection.getByText('GrandHyatt_Guest_5G')).toBeTruthy();
    expect(connection.getByText('10.15.82.114')).toBeTruthy();
    expect(connection.getByText('36 (5 GHz)')).toBeTruthy();
  });

  it('shows the cable when it carries the internet', () => {
    renderInternet((uplink) => {
      uplink.kind = 'ethernet';
      uplink.interface = 'eth0';
    });
    const connection = within(section('Connection'));
    expect(connection.getByText('Cable plugged in (eth0)')).toBeTruthy();
    expect(connection.getByText('1000 Mbps Full Duplex')).toBeTruthy();
    expect(connection.queryByText('BSSID')).toBeNull();
  });

  it('Renew lease and Disconnect ask the router', async () => {
    const { device, user } = renderInternet();
    await user.click(screen.getByRole('button', { name: 'Renew lease' }));
    await user.click(screen.getByRole('button', { name: 'Disconnect' }));
    expect(device.renewDhcpLease).toHaveBeenCalled();
    expect(device.disconnectUplink).toHaveBeenCalled();
  });

  // What the page says when there is no internet, by what RasBeacon reports.
  it.each([
    {
      when: 'the only radio runs the hotspot',
      change: (u: MockRouter['status']['uplink']) => (u.wifi.available = false),
      says: 'No Wi-Fi radio free for the internet',
    },
    {
      when: 'it is joining a network',
      change: (u: MockRouter['status']['uplink']) => (u.wifi.state = 'connecting'),
      says: 'RasBeacon is joining "GrandHyatt_Guest_5G".',
    },
    {
      when: 'the login was refused',
      change: (u: MockRouter['status']['uplink']) => (u.wifi.state = 'login-failed'),
      says: 'The network rejected the login',
    },
    {
      when: 'no network is saved',
      change: (u: MockRouter['status']['uplink']) => (u.wifi.state = 'not-configured'),
      says: 'No networks saved',
    },
    {
      when: 'no saved network is in range',
      change: (u: MockRouter['status']['uplink']) => (u.wifi.state = 'searching'),
      says: 'Looking for your saved networks',
    },
  ])('says why there is no internet when $when', ({ change, says }) => {
    renderInternet((uplink) => {
      uplink.connected = false;
      change(uplink);
    });
    expect(screen.getByText(says)).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Connection' })).toBeNull();
  });

  it('without a free radio, says a waiting cable hasn’t brought internet yet', () => {
    renderInternet((uplink) => {
      uplink.connected = false;
      uplink.wifi.available = false;
      uplink.ethernet.cablePlugged = true;
    });
    expect(screen.getByText('The Ethernet cable is plugged in, but no internet came through it yet.')).toBeTruthy();
  });
});

describe('Ethernet port', () => {
  it('asks before switching the port to a client port, then saves', async () => {
    const { device, user } = renderInternet();
    await user.click(screen.getByRole('radio', { name: /Client port/ }));
    const alert = within(screen.getByRole('dialog', { name: 'Use the port for your own device?' }));
    await user.click(alert.getByRole('button', { name: 'Switch now' }));
    expect(device.setEthernetRole).toHaveBeenCalledWith('client-port');
  });

  it('warns when a client port cable leads to a network', () => {
    renderInternet((uplink, router) => {
      router.uplinkSettings.ethernetRole = 'client-port';
      uplink.ethernet.role = 'client-port';
      uplink.ethernet.foreignDhcpServer = '192.168.1.1';
    });
    expect(
      screen.getByText(/A DHCP server answered on the client port \(192\.168\.1\.1\)/).closest('[role=alert]'),
    ).toBeTruthy();
  });

  it('shows the port’s details while it carries the internet', () => {
    renderInternet((_, router) => {
      router.status.ethernetDiagnostics = {
        carrierState: 'up',
        controller: 'Broadcom GENET',
        mtu: '1500',
        packetStatistics: '1 024 in, 980 out',
        dnsServers: '192.168.1.1',
        dhcpLease: '23 h left',
        portDescription: 'The Pi’s own Ethernet port.',
        pollingInterval: '5 seconds',
      };
    });
    const details = within(section('Port details'));
    expect(details.getByText('Broadcom GENET')).toBeTruthy();
    expect(details.getByText('The Pi’s own Ethernet port. Checked every 5 seconds.')).toBeTruthy();
  });

  it('Rescan port asks the router', async () => {
    const { device, user } = renderInternet();
    await user.click(screen.getByRole('button', { name: 'Rescan port' }));
    expect(device.rescanEthernetPort).toHaveBeenCalled();
  });
});
