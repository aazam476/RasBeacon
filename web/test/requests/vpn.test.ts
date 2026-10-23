// The VPN page: the switches and the WireGuard profiles.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import { testRequests } from './helpers';

describe('VPN', () => {
  testRequests([
    { name: 'reads the VPN settings', call: api.getVPNSettings, method: 'GET', path: '/settings/vpn' },
    {
      name: 'saves the switches',
      call: () => api.putVPNSwitches({ enabled: true, killSwitch: false }),
      method: 'PUT',
      path: '/settings/vpn',
      body: { enabled: true, killSwitch: false },
    },
    {
      name: 'imports a WireGuard file with its name',
      call: () => api.importVPNProfile('[Interface]\n', 'home.conf'),
      method: 'POST',
      path: '/settings/vpn/import',
      body: { conf: '[Interface]\n', name: 'home.conf' },
    },
    {
      name: 'renames a profile',
      call: () => api.renameVPNProfile('wireguard.conf', 'Home'),
      method: 'POST',
      path: '/settings/vpn/profile/rename',
      body: { name: 'wireguard.conf', newName: 'Home' },
    },
    {
      name: 'puts a profile in use',
      call: () => api.vpnProfileAction('activate', 'Home'),
      method: 'POST',
      path: '/settings/vpn/profile/activate',
      body: { name: 'Home' },
    },
    {
      name: 'removes a profile',
      call: () => api.vpnProfileAction('remove', 'Home'),
      method: 'POST',
      path: '/settings/vpn/profile/remove',
      body: { name: 'Home' },
    },
    {
      name: 'makes a new key',
      call: () => api.vpnProfileAction('generate-key', 'Home'),
      method: 'POST',
      path: '/settings/vpn/profile/generate-key',
      body: { name: 'Home' },
    },
    {
      name: 'saves a profile’s details',
      call: () =>
        api.saveVPNProfile({
          name: 'Home',
          addresses: ['10.66.0.2/32'],
          dns: ['10.66.0.1'],
          mtu: 1420,
          peerPublicKey: 'pub',
          endpoint: 'vpn.example.com:51820',
          allowedIps: ['0.0.0.0/0'],
          persistentKeepalive: 25,
          publicKey: '',
          hasPresharedKey: false,
          routedIpv6Prefix: '',
          privateKey: 'new-key',
        }),
      method: 'POST',
      path: '/settings/vpn/profile/save',
      body: {
        name: 'Home',
        addresses: ['10.66.0.2/32'],
        dns: ['10.66.0.1'],
        mtu: 1420,
        peerPublicKey: 'pub',
        endpoint: 'vpn.example.com:51820',
        allowedIps: ['0.0.0.0/0'],
        persistentKeepalive: 25,
        publicKey: '',
        hasPresharedKey: false,
        routedIpv6Prefix: '',
        privateKey: 'new-key',
      },
    },
  ]);
});
