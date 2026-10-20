// The Plugins pages: installing, the catalog, running, settings and sources.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import type { PluginChoices } from '@/data/types';
import { testFile, testRequests } from './helpers';

const choices: PluginChoices = { ports: { 'tcp/3000': 3000 }, settings: { password: 's3cret' }, drive: '' };

describe('plugins', () => {
  testRequests([
    { name: 'reads the installed plugins', call: api.getPlugins, method: 'GET', path: '/plugins' },
    {
      name: 'uploads a plugin file',
      call: () => api.uploadPlugin(testFile('x.rba')),
      method: 'POST',
      path: '/plugins/upload',
      body: '<file>',
    },
    {
      name: 'reads the catalog',
      call: () => api.getPluginCatalog(true),
      method: 'POST',
      path: '/plugins/catalog',
      body: { refresh: true },
    },
    {
      name: 'downloads a catalog plugin',
      call: () => api.downloadCatalogPlugin('aazam476/RasBeacon-plugins', 'adguard-home.rba'),
      method: 'POST',
      path: '/plugins/catalog/download',
      body: { source: 'aazam476/RasBeacon-plugins', file: 'adguard-home.rba' },
    },
    {
      name: 'downloads an update',
      call: () => api.downloadPluginUpdate('adguard-home'),
      method: 'POST',
      path: '/plugins/update',
      body: { id: 'adguard-home' },
    },
    {
      name: 'installs with the choices',
      call: () => api.installPlugin('abc123', choices),
      method: 'POST',
      path: '/plugins/install',
      body: { stagingId: 'abc123', ...choices },
    },
    {
      name: 'cancels an install',
      call: () => api.cancelPluginInstall('abc123'),
      method: 'POST',
      path: '/plugins/cancel',
      body: { stagingId: 'abc123' },
    },
    {
      name: 'saves a plugin’s settings',
      call: () => api.putPluginSettings('adguard-home', choices),
      method: 'PUT',
      path: '/plugins/settings',
      body: { id: 'adguard-home', ...choices },
    },
    {
      name: 'starts a plugin',
      call: () => api.pluginAction('start', 'adguard-home'),
      method: 'POST',
      path: '/plugins/start',
      body: { id: 'adguard-home' },
    },
    {
      name: 'stops a plugin',
      call: () => api.pluginAction('stop', 'adguard-home'),
      method: 'POST',
      path: '/plugins/stop',
      body: { id: 'adguard-home' },
    },
    {
      name: 'removes a plugin, keeping its data',
      call: () => api.removePlugin('adguard-home', true),
      method: 'POST',
      path: '/plugins/remove',
      body: { id: 'adguard-home', keepData: true },
    },
    {
      name: 'reads its output',
      call: () => api.getPluginLogs('adguard-home'),
      method: 'POST',
      path: '/plugins/logs',
      body: { id: 'adguard-home' },
    },
    { name: 'lists USB drives', call: api.getPluginDrives, method: 'GET', path: '/plugins/drives' },
    {
      name: 'saves the sources',
      call: () => api.putPluginSources(['someone/more-plugins']),
      method: 'PUT',
      path: '/plugins/sources',
      body: { sources: ['someone/more-plugins'] },
    },
  ]);
});
