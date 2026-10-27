// The Clients page: pausing, disconnecting, naming devices, and data use.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import { testRequests } from './helpers';

const mac = '3a:1f:22:aa:bb:cc';

describe('clients', () => {
  testRequests([
    { name: 'reads the status (with the devices)', call: api.getStatus, method: 'GET', path: '/status' },
    {
      name: 'pauses a device',
      call: () => api.setClientInternetAccess(mac, false),
      method: 'POST',
      path: '/clients/internet-access',
      body: { mac, allowed: false },
    },
    {
      name: 'disconnects a device',
      call: () => api.clientAction('disconnect', mac),
      method: 'POST',
      path: '/clients/disconnect',
      body: { mac },
    },
    {
      name: 'forgets its address',
      call: () => api.clientAction('flush-lease', mac),
      method: 'POST',
      path: '/clients/flush-lease',
      body: { mac },
    },
    {
      name: 'names a device',
      call: () => api.renameClient(mac, "Ali's phone"),
      method: 'POST',
      path: '/clients/name',
      body: { mac, name: "Ali's phone" },
    },
    {
      name: 'gives a device a fixed address',
      call: () => api.setFixedAddress(mac, '10.87.0.50'),
      method: 'POST',
      path: '/clients/fixed-address',
      body: { mac, address: '10.87.0.50' },
    },
  ]);
});

describe('data use', () => {
  testRequests([
    { name: 'reads the limit', call: api.getUsageSettings, method: 'GET', path: '/settings/usage' },
    {
      name: 'saves the limit',
      call: () => api.putUsageSettings(2000),
      method: 'PUT',
      path: '/settings/usage',
      body: { limitMb: 2000 },
    },
    { name: 'resets the counts', call: api.resetUsage, method: 'POST', path: '/clients/usage/reset' },
  ]);
});
