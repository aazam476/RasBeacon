// Before logging in: the session and the hotel login page. (Claiming sends two requests: see
// session.test.ts.)
import { describe } from 'vitest';
import * as api from '@/data/real/api';
import { testRequests } from './helpers';

describe('setup and login', () => {
  testRequests([
    { name: 'reads the setup state', call: api.getSetupState, method: 'GET', path: '/setup' },
    { name: 'reads the session', call: api.getSession, method: 'GET', path: '/session' },
    {
      name: 'logs in, staying logged in',
      call: () => api.logIn('tangerine-orbit-42', true),
      method: 'POST',
      path: '/session',
      body: { password: 'tangerine-orbit-42', stayLoggedIn: true },
    },
    { name: 'logs out', call: api.logOut, method: 'DELETE', path: '/session' },
  ]);
});

describe('hotel login pages', () => {
  testRequests([
    { name: 'reads the login page state', call: api.getPortalStatus, method: 'GET', path: '/portal' },
    {
      name: 'starts login mode',
      call: () => api.portalAction('start'),
      method: 'POST',
      path: '/portal/start',
      body: {},
    },
    { name: 'checks again', call: () => api.portalAction('check'), method: 'POST', path: '/portal/check', body: {} },
    {
      name: 'opens RasBeacon’s browser at the page size',
      call: () => api.startPortalBrowser({ width: 390, height: 700 }),
      method: 'POST',
      path: '/portal/browser/start',
      body: { width: 390, height: 700 },
    },
    { name: 'closes the browser', call: api.stopPortalBrowser, method: 'POST', path: '/portal/browser/stop', body: {} },
    {
      name: 'asks for a newer picture',
      call: () => api.getPortalBrowserFrame(7),
      method: 'GET',
      path: '/portal/browser/frame?after=7',
    },
    {
      name: 'sends a tap',
      call: () => api.sendPortalBrowserInput({ action: 'click', x: 10, y: 20 }),
      method: 'POST',
      path: '/portal/browser/input',
      body: { action: 'click', x: 10, y: 20 },
    },
  ]);
});
