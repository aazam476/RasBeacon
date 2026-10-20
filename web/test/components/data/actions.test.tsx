// Every action the real data layer offers the screens: the requests it sends, and what it does
// with the answer. The request tests (test/requests) check each request's exact form; these
// check that an action picks the right ones and keeps the state in step.
//
// To test a new action, add one line to the table:
//
//   { name: 'renames a device', run: (d) => d.renameClient('3a:…', 'Phone'),
//     sends: ['POST /clients/name'], body: { mac: '3a:…', name: 'Phone' } },
import { act, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Device } from '@/data/device';
import { FieldErrors } from '@/data/errors';
import { fakeRbweb, renderProvider, type FakeAnswer } from './provider';

/** A small file, for uploads. */
const testFile = (name: string) => new File(['data'], name);

interface ActionCase {
  name: string;
  run: (device: Device) => unknown;
  /** The requests the action sends, in order, as "METHOD /path" (the reloads of loaded data left out). */
  sends: string[];
  /** The body of the last request, when the action builds it from more than its arguments. */
  body?: unknown;
  /** How the fake answers the last request. */
  answer?: FakeAnswer;
  /** Checks the state, or what the action answered, afterwards. */
  then?: (device: Device, answered: unknown) => void;
}

/** Renders the provider logged in, once the settings have loaded. */
async function withReadyDevice(answers: Record<string, FakeAnswer>) {
  const rbweb = fakeRbweb({ answers });
  const device = renderProvider();
  await waitFor(() => expect(device().httpsSettings.data && device().uplinkSettings.data).toBeTruthy());
  await waitFor(() => expect(device().vpnSettings.data && device().status.data).toBeTruthy());
  return { ...rbweb, device };
}

function testActions(cases: ActionCase[]) {
  it.each(cases)('$name', async ({ run, sends, body, answer, then }) => {
    const last = sends[sends.length - 1];
    const { device, sent, loaded } = await withReadyDevice(answer ? { [last]: answer } : {});
    const before = sent.length;
    let answered: unknown;
    await act(async () => {
      answered = await run(device());
    });
    const own = () => sent.slice(before).filter((r) => !(r.method === 'GET' && loaded.has(r.path)));
    await waitFor(() => expect(own().map((r) => `${r.method} ${r.path}`)).toEqual(sends));
    if (body !== undefined) expect(own().at(-1)?.body).toEqual(body);
    then?.(device(), answered);
  });
}

describe('session', () => {
  testActions([
    {
      name: 'logging out forgets everything loaded',
      run: (d) => d.logOut(),
      sends: ['DELETE /session'],
      answer: { status: 204 },
      then: (d) => {
        expect(d.phase).toEqual({ kind: 'login' });
        expect(d.status.data).toBeUndefined();
        expect(d.vpnSettings.data).toBeUndefined();
      },
    },
  ]);

  it('logging in loads the settings', async () => {
    const { sent } = fakeRbweb({ loggedIn: false });
    const device = renderProvider();
    await waitFor(() => expect(device().phase.kind).toBe('login'));
    await act(() => device().logIn('tangerine-orbit-42', true));
    await waitFor(() => expect(device().phase.kind).toBe('ready'));
    expect(sent.find((r) => r.method === 'POST' && r.path === '/session')?.body).toEqual({
      password: 'tangerine-orbit-42',
      stayLoggedIn: true,
    });
    await waitFor(() => expect(device().hotspotSettings.data).toBeDefined());
  });

  it('a request answered 401 shows the login page', async () => {
    const { device } = await withReadyDevice({ 'POST /speedtest': { status: 401, body: { error: 'Log in again.' } } });
    await act(() =>
      device()
        .runSpeedtest()
        .catch(() => undefined),
    );
    expect(device().phase.kind).toBe('login');
  });
});

describe('Internet', () => {
  testActions([
    {
      name: 'saved networks go with the other uplink settings',
      run: (d) => d.saveWifiNetworks([]),
      sends: ['PUT /settings/uplink'],
    },
    {
      name: 'nearby networks are kept for the Add network sheet',
      run: (d) => d.scanNearbyNetworks(),
      sends: ['POST /uplink/scan'],
      answer: { body: { networks: [{ ssid: 'Cafe', signalDbm: -50, frequencyMhz: 2412, security: 'open' }] } },
      then: (d) => expect(d.nearbyNetworks?.map((n) => n.ssid)).toEqual(['Cafe']),
    },
    { name: 'disconnects', run: (d) => d.disconnectUplink(), sends: ['POST /uplink/disconnect'] },
    { name: 'renews the address', run: (d) => d.renewDhcpLease(), sends: ['POST /uplink/renew'] },
    {
      name: 'looks at the Ethernet port again',
      run: (d) => d.rescanEthernetPort(),
      sends: ['POST /uplink/ethernet/scan'],
    },
    {
      name: 'DNS servers are sent as a list, in the order typed',
      run: (d) => d.saveDnsSettings('9.9.9.9\n1.1.1.1,  https://dns.example/dns-query', true),
      sends: ['PUT /settings/dns'],
      body: { servers: ['9.9.9.9', '1.1.1.1', 'https://dns.example/dns-query'], withVpn: true },
      answer: { body: { servers: ['9.9.9.9'], withVpn: true } },
      then: (d) => expect(d.dnsSettings.data).toEqual({ servers: ['9.9.9.9'], withVpn: true }),
    },
    {
      name: 'the speed test result shows in the status',
      run: (d) => d.runSpeedtest(),
      sends: ['POST /speedtest'],
      answer: { body: { downloadMbps: 87 } },
      then: (d) => expect(d.status.data?.speedtest).toEqual({ downloadMbps: 87 }),
    },
  ]);

  it('the Ethernet role is saved with the other uplink settings', async () => {
    const { device, sent, router } = await withReadyDevice({});
    await act(() => device().setEthernetRole('client-port'));
    expect(sent.find((r) => r.method === 'PUT')?.body).toEqual({
      ...router.uplinkSettings,
      ethernetRole: 'client-port',
    });
  });

  it('a new network with problems is refused before anything is sent', async () => {
    const { device, sent } = await withReadyDevice({});
    const before = sent.length;
    const error = await device()
      .prepareNetwork(
        {
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
        },
        [],
      )
      .catch((e: unknown) => e);
    expect(error).toBeInstanceOf(FieldErrors);
    expect(sent.length).toBe(before);
  });
});

describe('VPN', () => {
  testActions([
    {
      name: 'turning the VPN off keeps the kill switch as it is',
      run: (d) => d.setVpnEnabled(false),
      sends: ['PUT /settings/vpn'],
      body: { enabled: false, killSwitch: true },
    },
    {
      name: 'turning the kill switch off keeps the VPN as it is',
      run: (d) => d.setKillSwitch(false),
      sends: ['PUT /settings/vpn'],
      body: { enabled: true, killSwitch: false },
    },
    {
      name: 'the router’s answer replaces the VPN settings',
      run: (d) => d.setActiveVpnProfile('Home'),
      sends: ['POST /settings/vpn/profile/activate'],
      answer: { body: { enabled: true, killSwitch: true, activeProfile: 'Home', profiles: [] } },
      then: (d) => expect(d.vpnSettings.data?.activeProfile).toBe('Home'),
    },
    { name: 'removes a profile', run: (d) => d.removeVpnProfile('Home'), sends: ['POST /settings/vpn/profile/remove'] },
    {
      name: 'makes a new key',
      run: (d) => d.generateVpnKey('Home'),
      sends: ['POST /settings/vpn/profile/generate-key'],
    },
    {
      name: 'imports a file with its name',
      run: (d) => d.importVpnProfile({ text: '[Interface]\n', fileName: 'home.conf' }),
      sends: ['POST /settings/vpn/import'],
      body: { conf: '[Interface]\n', name: 'home.conf' },
    },
  ]);
});

describe('hotspot, guests and who can join', () => {
  testActions([
    {
      name: 'saves the hotspot',
      run: (d) => d.saveHotspotSettings(d.hotspotSettings.data!),
      sends: ['PUT /settings/hotspot'],
    },
    {
      name: 'answers the hotspot password',
      run: (d) => d.revealHotspotPassword(),
      sends: ['POST /settings/hotspot/reveal-password'],
      answer: { body: { hotspotPassword: 'sunny-harbor-77' } },
      then: (_, answered) => expect(answered).toBe('sunny-harbor-77'),
    },
    {
      name: 'answers the QR code',
      run: (d) => d.wifiQr('guest'),
      sends: ['POST /wifi-qr'],
      body: { network: 'guest' },
      answer: { body: { svg: '<svg/>' } },
      then: (_, answered) => expect(answered).toEqual({ svg: '<svg/>' }),
    },
    {
      name: 'saves the guest network',
      run: (d) => d.saveGuestSettings(d.guestSettings.data!),
      sends: ['PUT /settings/guest'],
    },
  ]);
});

describe('clients', () => {
  const mac = '3a:1f:22:aa:bb:cc';
  testActions([
    {
      name: 'pauses a device’s internet',
      run: (d) => d.setClientInternetAccess(mac, false),
      sends: ['POST /clients/internet-access'],
      body: { mac, allowed: false },
    },
    { name: 'disconnects a device', run: (d) => d.disconnectClient(mac), sends: ['POST /clients/disconnect'] },
    { name: 'forgets its address', run: (d) => d.flushClientLease(mac), sends: ['POST /clients/flush-lease'] },
    { name: 'renames it', run: (d) => d.renameClient(mac, 'Phone'), sends: ['POST /clients/name'] },
    {
      name: 'saves the data limit',
      run: (d) => d.saveUsageLimit(500),
      sends: ['PUT /settings/usage'],
      answer: { body: { limitMb: 500, since: '2026-10-19T00:00:00Z' } },
      then: (d) => expect(d.usageSettings.data).toEqual({ limitMb: 500, since: '2026-10-19T00:00:00Z' }),
    },
    { name: 'resets the counts', run: (d) => d.resetUsage(), sends: ['POST /clients/usage/reset'] },
  ]);
});

describe('SSH and HTTPS', () => {
  testActions([
    { name: 'saves SSH', run: (d) => d.saveSshSettings(d.sshSettings.data!), sends: ['PUT /settings/ssh'] },
    {
      name: 'turns HTTPS off',
      run: (d) => d.setHttpsEnabled(false),
      sends: ['PUT /settings/https'],
      body: { enabled: false },
    },
    {
      name: 'a new certificate keeps HTTPS on',
      run: (d) => d.makeNewCertificate(),
      sends: ['PUT /settings/https'],
      body: { enabled: true, generateNew: true },
    },
    {
      name: 'an uploaded certificate is sent as text',
      run: (d) =>
        d.uploadCertificate({
          certificate: new File(['CERT'], 'cert.pem'),
          privateKey: new File(['KEY'], 'key.pem'),
        }),
      sends: ['PUT /settings/https'],
      body: { enabled: true, certificate: 'CERT', privateKey: 'KEY' },
    },
  ]);
});

describe('System', () => {
  testActions([
    {
      name: 'an installed update shows in the status',
      run: (d) => d.installUpdate(testFile('x.rbupdate')),
      sends: ['POST /system/update'],
      body: '<file>',
      answer: { body: { state: 'installed' } },
      then: (d) => expect(d.status.data?.update).toEqual({ state: 'installed' }),
    },
    { name: 'resizes the swap file', run: (d) => d.saveSwapSettings(2), sends: ['PUT /settings/swap'] },
    {
      name: 'keeps the saved update schedule',
      run: (d) => d.saveUpdateSettings({ checkHours: 168, notifyUrl: '' }),
      sends: ['PUT /settings/updates'],
      then: (d) => expect(d.updateSettings.data).toEqual({ checkHours: 168, notifyUrl: '' }),
    },
    {
      name: 'checking for updates shows the answer in the status',
      run: (d) => d.checkForUpdates(),
      sends: ['POST /system/update/check'],
      answer: { body: { latest: '0.3.0' } },
      then: (d) => expect(d.status.data?.update).toEqual({ latest: '0.3.0' }),
    },
    {
      name: 'sends a test notification',
      run: (d) => d.testUpdateNotification('https://ntfy.sh/rasbeacon'),
      sends: ['POST /settings/updates/test'],
    },
    {
      name: 'restoring uploads the file, then restores with the password',
      run: (d) => d.restore(testFile('x.rbbackup'), 'backup-pass-42'),
      sends: ['POST /system/restore/upload', 'POST /system/restore'],
      body: { password: 'backup-pass-42' },
    },
  ]);
});

describe('plugins', () => {
  const choices = { ports: { 'tcp/3000': 3000 }, settings: {}, drive: '' };
  testActions([
    {
      name: 'previews a file',
      run: (d) => d.previewPluginFile(testFile('x.rba')),
      sends: ['POST /plugins/upload'],
      body: '<file>',
    },
    { name: 'reads the catalog', run: (d) => d.pluginCatalog(true), sends: ['POST /plugins/catalog'] },
    {
      name: 'previews a catalog plugin',
      run: (d) => d.previewCatalogPlugin('https://example.com/plugins', 'adguard.rba'),
      sends: ['POST /plugins/catalog/download'],
    },
    { name: 'previews an update', run: (d) => d.previewPluginUpdate('adguard'), sends: ['POST /plugins/update'] },
    {
      name: 'installs, then reloads the plugins',
      run: (d) => d.installPlugin('staged-1', choices),
      sends: ['POST /plugins/install'],
      body: { stagingId: 'staged-1', ...choices },
    },
    {
      name: 'cancelling an install never fails',
      run: (d) => d.cancelPluginInstall('staged-1'),
      sends: ['POST /plugins/cancel'],
      answer: { status: 500, body: { error: 'Gone.' } },
    },
    {
      name: 'saves a plugin’s settings',
      run: (d) => d.savePluginSettings('adguard', choices),
      sends: ['PUT /plugins/settings'],
    },
    { name: 'stops a plugin', run: (d) => d.setPluginRunning('adguard', false), sends: ['POST /plugins/stop'] },
    { name: 'starts a plugin', run: (d) => d.setPluginRunning('adguard', true), sends: ['POST /plugins/start'] },
    {
      name: 'removes a plugin, keeping its data',
      run: (d) => d.removePlugin('adguard', true),
      sends: ['POST /plugins/remove'],
      body: { id: 'adguard', keepData: true },
    },
    {
      name: 'answers a plugin’s log lines',
      run: (d) => d.pluginLogs('adguard'),
      sends: ['POST /plugins/logs'],
      answer: { body: { lines: ['started'] } },
      then: (_, answered) => expect(answered).toEqual(['started']),
    },
    {
      name: 'answers the USB drives',
      run: (d) => d.pluginDrives(),
      sends: ['GET /plugins/drives'],
      answer: { body: { drives: [] } },
      then: (_, answered) => expect(answered).toEqual([]),
    },
    {
      name: 'saves the sources',
      run: (d) => d.savePluginSources(['https://example.com/plugins']),
      sends: ['PUT /plugins/sources'],
    },
  ]);
});

describe('login pages', () => {
  testActions([
    { name: 'checks for a login page', run: (d) => d.checkPortal(), sends: ['POST /portal/check'] },
    { name: 'gives login mode more time', run: (d) => d.extendLoginMode(), sends: ['POST /portal/extend'] },
    { name: 'stops login mode', run: (d) => d.stopLoginMode(), sends: ['POST /portal/stop'] },
    { name: 'opens RasBeacon’s browser', run: (d) => d.openPortalBrowser(), sends: ['POST /portal/browser/start'] },
    { name: 'closes it', run: (d) => d.closePortalBrowser(), sends: ['POST /portal/browser/stop'] },
  ]);

  /** The browser open, showing its first picture of an 800 × 600 page. */
  async function withBrowserOpen() {
    const rbweb = fakeRbweb({
      answers: {
        'GET /portal': { body: { ...fakeRbweb().router.status.portal, browsing: true } },
        'GET /portal/browser/frame?after=0': {
          body: {
            running: true,
            seq: 1,
            image: 'abc',
            width: 800,
            height: 600,
            url: 'http://hotel.example',
            title: 'Hotel',
          },
        },
        // The next picture never comes, so the first stays.
        'GET /portal/browser/frame?after=1': { hang: true },
      },
    });
    const device = renderProvider('/portal');
    await waitFor(() => expect(device().portalBrowser.imageSrc).toBe('data:image/jpeg;base64,abc'));
    const inputs = () => rbweb.sent.filter((r) => r.path === '/portal/browser/input').map((r) => r.body);
    return { device, inputs };
  }

  // The picture is shown at half size, at the top left of the screen.
  const box = { left: 0, top: 0, width: 400, height: 300 };

  it('shows each new picture of the page', async () => {
    const { device } = await withBrowserOpen();
    expect(device().portalBrowser.frame).toMatchObject({ url: 'http://hotel.example', title: 'Hotel' });
  });

  it('a tap clicks the same spot on the page', async () => {
    const { device, inputs } = await withBrowserOpen();
    act(() => {
      device().portalPointerDown({ clientX: 100, clientY: 50 });
      device().portalPointerUp({ clientX: 101, clientY: 50 }, box);
    });
    await waitFor(() => expect(inputs()).toEqual([{ action: 'click', x: 200, y: 100 }]));
  });

  it('a swipe up scrolls the page down', async () => {
    const { device, inputs } = await withBrowserOpen();
    act(() => {
      device().portalPointerDown({ clientX: 100, clientY: 200 });
      device().portalPointerUp({ clientX: 100, clientY: 100 }, box);
    });
    await waitFor(() => expect(inputs()).toEqual([{ action: 'scroll', x: 200, y: 400, deltaY: 200 }]));
  });

  it('typing, keys, the wheel, back and reload go to the page', async () => {
    const { device, inputs } = await withBrowserOpen();
    act(() => {
      device().portalType('');
      device().portalType('room 214');
      device().portalKey('Enter');
      device().portalWheel({ clientX: 100, clientY: 50 }, box, 120.4);
      device().portalBack();
      device().portalReload();
    });
    await waitFor(() =>
      expect(inputs()).toEqual([
        { action: 'type', text: 'room 214' },
        { action: 'key', key: 'Enter' },
        { action: 'scroll', x: 200, y: 100, deltaY: 120 },
        { action: 'back' },
        { action: 'reload' },
      ]),
    );
  });
});
