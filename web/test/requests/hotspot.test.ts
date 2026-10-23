// The Hotspot and Guests pages: the networks, the password and QR codes, who can join.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import type { AccessSettings, GuestSettings, HotspotSettings } from '@/data/types';
import { testRequests } from './helpers';

const hotspot: HotspotSettings = {
  ssid: 'Travel',
  band: '5GHz',
  channel: 44,
  security: 'wpa3',
  country: 'US',
  metered: false,
  password: '',
};
const guest: GuestSettings = {
  enabled: true,
  ssid: 'Guests',
  password: 'visitors-welcome',
  hasPassword: false,
  supported: true,
};
const access: AccessSettings = { allowListOn: true, allowed: ['3a:1f:22:aa:bb:cc'], maxDevices: 8 };

describe('hotspot', () => {
  testRequests([
    { name: 'reads the hotspot settings', call: api.getHotspotSettings, method: 'GET', path: '/settings/hotspot' },
    {
      name: 'saves them',
      call: () => api.putHotspotSettings(hotspot),
      method: 'PUT',
      path: '/settings/hotspot',
      body: hotspot,
    },
    // Being logged in is enough: no admin password is sent.
    {
      name: 'shows the password',
      call: api.revealHotspotPassword,
      method: 'POST',
      path: '/settings/hotspot/reveal-password',
      body: {},
    },
    {
      name: 'asks which channels a country allows',
      call: () => api.getHotspotChannels('JP'),
      method: 'POST',
      path: '/settings/hotspot/channels',
      body: { country: 'JP' },
    },
    {
      name: 'makes the QR code',
      call: () => api.wifiQr('hotspot'),
      method: 'POST',
      path: '/wifi-qr',
      body: { network: 'hotspot' },
    },
  ]);
});

describe('who can join', () => {
  testRequests([
    { name: 'reads the allow list and limit', call: api.getAccessSettings, method: 'GET', path: '/settings/access' },
    {
      name: 'saves them',
      call: () => api.putAccessSettings(access),
      method: 'PUT',
      path: '/settings/access',
      body: access,
    },
  ]);
});

describe('guests', () => {
  testRequests([
    { name: 'reads the guest network', call: api.getGuestSettings, method: 'GET', path: '/settings/guest' },
    { name: 'saves it', call: () => api.putGuestSettings(guest), method: 'PUT', path: '/settings/guest', body: guest },
    {
      name: 'makes its QR code',
      call: () => api.wifiQr('guest'),
      method: 'POST',
      path: '/wifi-qr',
      body: { network: 'guest' },
    },
  ]);
});
