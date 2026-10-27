// The System, SSH and HTTPS pages: updates, swap, the name, restarting, backups.
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import { testFile, testRequests } from './helpers';

describe('updates', () => {
  testRequests([
    {
      name: 'installs an update file',
      call: () => api.installUpdate(testFile('x.rbupdate')),
      method: 'POST',
      path: '/system/update',
      body: '<file>',
    },
    { name: 'reads how often to check', call: api.getUpdateSettings, method: 'GET', path: '/settings/updates' },
    {
      name: 'saves the schedule and notification address',
      call: () => api.putUpdateSettings({ checkHours: 24, notifyUrl: 'https://ntfy.sh/rasbeacon' }),
      method: 'PUT',
      path: '/settings/updates',
      body: { checkHours: 24, notifyUrl: 'https://ntfy.sh/rasbeacon' },
    },
    { name: 'checks now', call: api.checkForUpdates, method: 'POST', path: '/system/update/check' },
    {
      name: 'restarts into an installed update',
      call: api.restartIntoUpdate,
      method: 'POST',
      path: '/system/update/restart',
    },
    {
      name: 'sends a test notification',
      call: () => api.testUpdateNotification('https://ntfy.sh/rasbeacon'),
      method: 'POST',
      path: '/settings/updates/test',
      body: { url: 'https://ntfy.sh/rasbeacon' },
    },
  ]);
});

describe('device', () => {
  testRequests([
    { name: 'reads the swap file', call: api.getSwapSettings, method: 'GET', path: '/settings/swap' },
    {
      name: 'resizes it',
      call: () => api.putSwapSettings(2),
      method: 'PUT',
      path: '/settings/swap',
      body: { sizeGb: 2, maxGb: 0 },
    },
    {
      name: 'renames RasBeacon',
      call: () => api.saveHostname('travel-pi'),
      method: 'POST',
      path: '/system/hostname',
      body: { hostname: 'travel-pi' },
    },
    { name: 'reboots', call: api.reboot, method: 'POST', path: '/system/reboot' },
    {
      name: 'sets what each radio does',
      call: () => api.putRadioRoles({ '00:0e:8e:00:00:03': 'guest' }),
      method: 'PUT',
      path: '/settings/radios',
      body: { roles: { '00:0e:8e:00:00:03': 'guest' } },
    },
    { name: 'keeps the radios’ new jobs', call: api.keepRadioRoles, method: 'POST', path: '/settings/radios/keep' },
    { name: 'goes back to the old jobs', call: api.revertRadioRoles, method: 'POST', path: '/settings/radios/revert' },
    { name: 'shuts down', call: api.shutDown, method: 'POST', path: '/system/shutdown' },
    {
      name: 'changes the admin password',
      call: () => api.changeAdminPassword('tangerine-orbit-42', 'plum-harbor-sunrise-7'),
      method: 'POST',
      path: '/system/admin-password',
      body: { currentPassword: 'tangerine-orbit-42', newPassword: 'plum-harbor-sunrise-7' },
    },
    {
      name: 'erases everything with the admin password',
      call: () => api.factoryReset('tangerine-orbit-42'),
      method: 'POST',
      path: '/system/factory-reset',
      body: { password: 'tangerine-orbit-42' },
    },
  ]);
});

describe('backups', () => {
  // No admin password: being logged in is enough.
  testRequests([
    {
      name: 'asks for a backup with its plugin choice',
      call: () => api.prepareBackup('backup-pass-42', 'data'),
      method: 'POST',
      path: '/system/backup',
      body: { password: 'backup-pass-42', plugins: 'data' },
    },
    {
      name: 'uploads a backup',
      call: () => api.uploadBackup(testFile('x.rbbackup')),
      method: 'POST',
      path: '/system/restore/upload',
      body: '<file>',
    },
    {
      name: 'restores it',
      call: () => api.restoreBackup('backup-pass-42'),
      method: 'POST',
      path: '/system/restore',
      body: { password: 'backup-pass-42' },
    },
  ]);
});

describe('SSH and HTTPS', () => {
  testRequests([
    { name: 'reads SSH', call: api.getSSHSettings, method: 'GET', path: '/settings/ssh' },
    {
      name: 'saves SSH',
      call: () => api.putSSHSettings({ enabled: true, authorizedKeys: 'ssh-ed25519 AAAA me@laptop' }),
      method: 'PUT',
      path: '/settings/ssh',
      body: { enabled: true, authorizedKeys: 'ssh-ed25519 AAAA me@laptop' },
    },
    { name: 'reads HTTPS', call: api.getHTTPSSettings, method: 'GET', path: '/settings/https' },
    {
      name: 'makes a new certificate',
      call: () => api.putHTTPSSettings({ enabled: true, generateNew: true }),
      method: 'PUT',
      path: '/settings/https',
      body: { enabled: true, generateNew: true },
    },
  ]);
});
