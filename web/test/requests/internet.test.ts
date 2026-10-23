// The Internet page: saved networks, nearby networks, the uplink buttons, DNS and the speed test.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import type { DNSSettings, UplinkSettings } from '@/data/types';
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
