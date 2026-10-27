// The Internet page: saved networks, nearby networks, the uplink buttons, DNS and the speed test.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import type { DNSSettings, ModemSettings, PortForwardRule, UplinkSettings, WelcomeSettings } from '@/data/types';
import { testRequests } from './helpers';

const uplink: UplinkSettings = {
  ethernetRole: 'uplink',
  networks: [],
  ethernetPresent: true,
  wifiUplinkAvailable: true,
};
const dns: DNSSettings = { servers: ['https://cloudflare-dns.com/dns-query', '1.1.1.1'], withVpn: true };

describe('internet', () => {
  testRequests([
    { name: 'reads the saved networks', call: api.getUplinkSettings, method: 'GET', path: '/settings/uplink' },
    {
      name: 'saves the networks',
      call: () => api.putUplinkSettings(uplink),
      method: 'PUT',
      path: '/settings/uplink',
      body: uplink,
    },
    { name: 'looks for nearby networks', call: api.scanNearbyNetworks, method: 'POST', path: '/uplink/scan' },
    {
      name: 'leaves the network',
      call: () => api.uplinkAction('disconnect'),
      method: 'POST',
      path: '/uplink/disconnect',
    },
    { name: 'renews the lease', call: () => api.uplinkAction('renew'), method: 'POST', path: '/uplink/renew' },
    {
      name: 'checks the Ethernet port',
      call: () => api.uplinkAction('ethernet/scan'),
      method: 'POST',
      path: '/uplink/ethernet/scan',
    },
    { name: 'runs the speed test', call: api.runSpeedtest, method: 'POST', path: '/speedtest' },
  ]);
});

describe('DNS', () => {
  testRequests([
    { name: 'reads the DNS servers', call: api.getDNSSettings, method: 'GET', path: '/settings/dns' },
    // The order matters (the first server is asked first), so it is sent as typed.
    {
      name: 'saves them in order',
      call: () => api.putDNSSettings(dns),
      method: 'PUT',
      path: '/settings/dns',
      body: dns,
    },
  ]);
});

describe('port forwarding', () => {
  const rule: PortForwardRule = {
    id: '',
    name: 'Minecraft',
    protocol: 'tcp',
    outsidePort: 25565,
    mac: 'a4:83:e7:12:34:56',
    enabled: true,
  };
  testRequests([
    { name: 'reads the rules', call: api.getPortForwarding, method: 'GET', path: '/settings/port-forwarding' },
    // The whole list goes in one request, as the router keeps it.
    {
      name: 'saves the whole list',
      call: () => api.putPortForwarding([rule]),
      method: 'PUT',
      path: '/settings/port-forwarding',
      body: { rules: [rule] },
    },
  ]);
});

describe('welcome page', () => {
  const settings: WelcomeSettings = {
    enabled: true,
    networks: 'guest',
    title: 'Welcome',
    message: '',
    terms: 'Be kind.',
    accessCode: '4711',
    askAgainHours: 24,
    includeWired: false,
    color: '#0c8a9c',
    logo: '',
    customPage: '',
    accepted: 0,
  };
  testRequests([
    { name: 'reads its settings', call: api.getWelcomeSettings, method: 'GET', path: '/settings/welcome' },
    {
      name: 'saves them',
      call: () => api.putWelcomeSettings(settings),
      method: 'PUT',
      path: '/settings/welcome',
      body: settings,
    },
    {
      name: 'asks a device again',
      call: () => api.resetWelcome('3a:1f:22:aa:bb:cc'),
      method: 'POST',
      path: '/clients/welcome-reset',
      body: { mac: '3a:1f:22:aa:bb:cc' },
    },
  ]);
});

describe('mobile modem', () => {
  const settings: ModemSettings = { apn: 'internet', username: '', hasPassword: false, hasPin: false, pin: '1234' };
  testRequests([
    { name: 'reads its settings', call: api.getModemSettings, method: 'GET', path: '/settings/modem' },
    {
      name: 'saves them',
      call: () => api.putModemSettings(settings),
      method: 'PUT',
      path: '/settings/modem',
      body: settings,
    },
  ]);
});
