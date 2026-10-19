import { describe, expect, it } from 'vitest';
import { FieldErrors } from '../errors';
import type { NewNetworkForm, SavedNetwork, SetupState, VPNDetailsForm } from '../types';
import { ApiError } from './api';
import {
  certificatesToUpload,
  checkAdminPassword,
  checkGuestForm,
  checkNewNetwork,
  checkSetupForm,
  checkSsid,
  checkWifiPassword,
  classifyGesture,
  decidePhase,
  fileToUpload,
  networkErrorKey,
  pageSizeFor,
  screenError,
  splitList,
  toSavedNetwork,
  vpnDetailsRequest,
} from './logic';

const normal: SetupState = {
  mode: 'normal',
  claimed: true,
  setupNetworkSecured: false,
  deviceShortId: '4F2A',
  lanAddress: '10.87.0.1',
};

describe('decidePhase', () => {
  it('waits for both answers', () => {
    expect(decidePhase(undefined, undefined, null)).toEqual({ kind: 'loading' });
    expect(decidePhase(normal, undefined, null)).toEqual({ kind: 'loading' });
  });
  it("says so when RasBeacon doesn't answer", () => {
    expect(decidePhase(undefined, undefined, 'offline')).toEqual({ kind: 'unreachable', error: 'offline' });
  });
  it('shows setup, notices, login and the pages', () => {
    expect(decidePhase({ ...normal, mode: 'setup', claimed: false }, false, null)).toEqual({ kind: 'setup' });
    expect(decidePhase({ ...normal, mode: 'setup-error', claimed: false }, false, null)).toEqual({
      kind: 'notice',
      notice: 'setup-error',
    });
    expect(decidePhase(normal, false, null)).toEqual({ kind: 'login' });
    expect(decidePhase(normal, true, null)).toEqual({ kind: 'ready' });
  });
  it('lets a logged-in owner past problems that only matter before setup', () => {
    expect(decidePhase({ ...normal, mode: 'no-radio' }, true, null)).toEqual({ kind: 'ready' });
  });
  it('keeps the phase when a later refresh fails', () => {
    expect(decidePhase(normal, true, 'offline')).toEqual({ kind: 'ready' });
  });
});

describe('input checks', () => {
  it('checks network names in bytes', () => {
    expect(checkSsid('Café')).toBeNull();
    expect(checkSsid('')).toMatch(/1 to 32 bytes/);
    expect(checkSsid('é'.repeat(17))).toMatch(/1 to 32 bytes/);
    expect(checkSsid('tab\there')).toMatch(/control/);
  });
  it('checks Wi-Fi and admin passwords', () => {
    expect(checkWifiPassword('short')).toBe('Must be 8 to 63 characters long.');
    expect(checkWifiPassword('naïve-password')).toMatch(/ASCII/);
    expect(checkWifiPassword('espresso-1')).toBeNull();
    expect(checkAdminPassword('short')).toMatch(/at least 10/);
    expect(checkAdminPassword('my-RasBeacon-pw')).toMatch(/device name/);
  });
  it('checks the setup form', () => {
    const form = {
      adminPassword: 'tangerine-orbit-42',
      adminPasswordConfirm: 'different',
      hotspotSsid: 'Ali',
      hotspotPassword: 'kq7m-2xvd-h8pt-rn4w',
      hotspotBand: '2.4GHz' as const,
    };
    expect(checkSetupForm(form)).toEqual({ adminPasswordConfirm: "The two passwords don't match." });
    expect(checkSetupForm({ ...form, adminPasswordConfirm: form.adminPassword })).toEqual({});
  });
  it('checks the guest form only when it is on', () => {
    const guests = { enabled: true, ssid: 'Guests', hasPassword: false, supported: true };
    expect(checkGuestForm(guests)).toEqual({ password: 'Choose a password for the guest network.' });
    expect(checkGuestForm({ ...guests, password: 'visitors-welcome' })).toEqual({});
    expect(checkGuestForm({ ...guests, hasPassword: true })).toEqual({});
    expect(checkGuestForm({ ...guests, enabled: false })).toEqual({});
  });
});

const emptyForm: NewNetworkForm = {
  ssid: '',
  security: 'personal',
  password: '',
  hidden: false,
  method: 'ttls-pap',
  identity: '',
  anonymousIdentity: '',
  serverName: '',
  caCertFiles: [],
  clientCertFile: null,
  clientKeyFile: null,
  allowNoCertificateCheck: false,
};
const home: SavedNetwork = { id: 'a', ssid: 'Home', security: 'personal', hidden: false, hasPassword: true };
const PEM = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n';

describe('adding a network', () => {
  it('checks personal and open networks, and refuses duplicates', () => {
    expect(checkNewNetwork({ ...emptyForm, ssid: 'Cafe', password: 'espresso-1' }, [home])).toEqual({});
    expect(checkNewNetwork({ ...emptyForm, ssid: 'Cafe' }, []).password).toBeTruthy();
    expect(checkNewNetwork({ ...emptyForm, ssid: 'Cafe', security: 'open' }, [])).toEqual({});
    expect(checkNewNetwork({ ...emptyForm, ssid: 'Home', security: 'open' }, [home]).ssid).toMatch(/already/);
  });
  it('takes Enterprise networks without a CA certificate or server name, but checks a name given', () => {
    const eduroam = { ...emptyForm, ssid: 'eduroam', security: 'enterprise' as const, identity: 'me', password: 'pw' };
    expect(checkNewNetwork(eduroam, [])).toEqual({});
    expect(checkNewNetwork({ ...eduroam, serverName: 'not a name!' }, []).serverName).toMatch(/radius\.example/);
    expect(checkNewNetwork({ ...eduroam, allowNoCertificateCheck: true }, [])).toEqual({});
    expect(
      Object.keys(checkNewNetwork({ ...eduroam, method: 'tls', allowNoCertificateCheck: true }, [])).sort(),
    ).toEqual(['clientCert', 'clientKey']);
  });
  it('reads the chosen files into the list entry', async () => {
    const network = await toSavedNetwork({
      ...emptyForm,
      ssid: 'eduroam',
      security: 'enterprise',
      identity: 'me@example.edu',
      password: 'pw',
      serverName: ' *.example.edu ',
      caCertFiles: [new File([PEM], 'campus.pem'), new File([new Uint8Array([0x30, 0x82])], 'extra.der')],
    });
    expect(network.enterprise?.serverName).toBe('*.example.edu');
    expect(network.enterprise?.caCertSummary).toBe('campus.pem, extra.der');
    expect(network.enterprise?.caCert).toBe(PEM + '-----BEGIN CERTIFICATE-----\nMII=\n-----END CERTIFICATE-----\n');
    expect(network.enterprise?.clientCert).toBe('');
  });
  it('drops the password of open networks', async () => {
    const network = await toSavedNetwork({ ...emptyForm, ssid: 'x', security: 'open', password: 'left', hidden: true });
    expect(network).toEqual({ id: '', ssid: 'x', security: 'open', hidden: true, hasPassword: false, password: '' });
  });
  it('sends PEM as text and DER as base64', () => {
    expect(fileToUpload(new TextEncoder().encode(PEM))).toBe(PEM);
    expect(fileToUpload(new Uint8Array([1, 2, 3]))).toBe('AQID');
    expect(certificatesToUpload([new TextEncoder().encode(PEM)])).toBe(PEM);
  });
});

describe('the VPN server details', () => {
  const form: VPNDetailsForm = {
    endpoint: ' vpn.example.com:51820 ',
    peerPublicKey: 'xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=',
    addresses: '10.66.0.2/32, fd66::2/128',
    dns: '10.66.0.1',
    allowedIps: '0.0.0.0/0,::/0',
    persistentKeepalive: '',
    mtu: '1420',
    routedIpv6Prefix: '',
    presharedKey: '',
    privateKey: '',
  };
  it('turns the form into the request for one profile', () => {
    expect(splitList(' a, b,,c ')).toEqual(['a', 'b', 'c']);
    expect(splitList('a\nb,\n c\n')).toEqual(['a', 'b', 'c']);
    const details = vpnDetailsRequest('home.conf', form);
    expect(details).toMatchObject({
      name: 'home.conf',
      endpoint: 'vpn.example.com:51820',
      addresses: ['10.66.0.2/32', 'fd66::2/128'],
      allowedIps: ['0.0.0.0/0', '::/0'],
      mtu: 1420,
      persistentKeepalive: 0,
    });
  });
  it('refuses numbers that are not numbers', () => {
    expect(() => vpnDetailsRequest('home.conf', { ...form, mtu: 'big' })).toThrow(FieldErrors);
  });
});

describe('the login page picture', () => {
  const box = { left: 0, top: 100, width: 195, height: 300 };
  const page = { width: 390, height: 600 };
  it('turns a tap into a click on the page, scaled', () => {
    expect(classifyGesture({ clientX: 50, clientY: 150 }, { clientX: 52, clientY: 151 }, box, page)).toEqual({
      action: 'click',
      x: 100,
      y: 100,
    });
  });
  it('turns a swipe up into scrolling down', () => {
    expect(classifyGesture({ clientX: 50, clientY: 300 }, { clientX: 50, clientY: 200 }, box, page)).toEqual({
      action: 'scroll',
      x: 100,
      y: 400,
      deltaY: 200,
    });
  });
  it('asks for a page as wide as the screen', () => {
    expect(pageSizeFor(390, 844)).toEqual({ width: 390, height: 591 });
  });
});

describe('refusals', () => {
  it('puts a message next to the input the router named', () => {
    const error = screenError(new ApiError(400, 'Too long.', 'hotspot.ssid'), { 'hotspot.ssid': 'ssid' });
    expect(error).toBeInstanceOf(FieldErrors);
    expect((error as FieldErrors).fields).toEqual({ ssid: 'Too long.' });
  });
  it('shows a saved network refusal at its position in the list', () => {
    expect(networkErrorKey('networks.2.password')).toBe('networks.2');
    expect(networkErrorKey('networks.0.enterprise.caCert')).toBe('networks.0');
    expect(networkErrorKey('hotspot.ssid')).toBeNull();
    expect((screenError(new ApiError(400, 'Wrong.', 'networks.1.password')) as FieldErrors).fields).toEqual({
      'networks.1': 'Wrong.',
    });
  });
  it('keeps other refusals as plain messages', () => {
    const error = screenError(new ApiError(403, 'Wrong password.', 'password'));
    expect(error).not.toBeInstanceOf(FieldErrors);
    expect(error.message).toBe('Wrong password.');
  });
});
