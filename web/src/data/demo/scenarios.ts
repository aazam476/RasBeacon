import type { AppPhase, DataKey, PortalBrowserState } from '../device';
import type { NearbyNetwork, PortalStatus } from '../types';
import { LOGIN_PAGE_PICTURE } from './loginPagePicture';
import { createMockRouter, type MockRouter } from './mockData';
import { applyNoClients, applyOneOfEach, applyWorstCase } from './worstCase';

/** Everything the demo provider keeps: the sample router plus which data "failed to load". */
export interface DemoState {
  phase: AppPhase;
  router: MockRouter;
  /** Data that never finishes loading in this scenario. */
  loading: Partial<Record<DataKey, true>>;
  /** Data whose last refresh failed with this message (with or without older data). */
  errors: Partial<Record<DataKey, string>>;
  nearbyNetworks: NearbyNetwork[] | null;
  portalBrowser: PortalBrowserState;
  /** Every action that changes something fails, to show how each control reports it. */
  failActions: boolean;
}

export interface DemoScenario {
  id: string;
  /** Heading on the demo page, e.g. "Status". */
  group: string;
  title: string;
  /** The page the scenario opens on. */
  route: string;
  apply?: (demo: DemoState) => void;
}

export const UNREACHABLE = "Can't reach RasBeacon. Check that this device is connected to it.";

export const CLOSED_BROWSER: PortalBrowserState = { frame: undefined, imageSrc: '', error: null };

/** The demo as it starts: logged in, everything working. */
export function createDemoState(): DemoState {
  const router = createMockRouter();
  return {
    phase: { kind: 'ready' },
    router,
    loading: {},
    errors: {},
    nearbyNetworks: router.nearbyNetworks,
    portalBrowser: CLOSED_BROWSER,
    failActions: false,
  };
}

/** The login page detected on the uplink: both ways to log in offered, the VPN waiting. */
function detectLoginPage(demo: DemoState, portal: Partial<PortalStatus> = {}) {
  const { status } = demo.router;
  status.portal = {
    detected: true,
    loginUrl: 'http://login.grandhyatt-guest.example/',
    canStart: true,
    loginMode: false,
    secondsLeft: 0,
    canBrowse: true,
    browsing: false,
    ...portal,
  };
  status.vpn = { ...status.vpn, state: 'connecting', handshakeAgeSeconds: -1, receivedBytes: 0, sentBytes: 2_048 };
}

function openBrowser(demo: DemoState, withPicture: boolean) {
  detectLoginPage(demo, { browsing: true });
  demo.portalBrowser = withPicture
    ? {
        frame: {
          running: true,
          seq: 7,
          width: 390,
          height: 591,
          url: 'http://login.grandhyatt-guest.example/',
          title: 'Guest Wi-Fi Login',
        },
        imageSrc: LOGIN_PAGE_PICTURE,
        error: null,
      }
    : CLOSED_BROWSER;
}

function disconnectUplink(demo: DemoState) {
  const { uplink } = demo.router.status;
  Object.assign(uplink, { connected: false, kind: '', interface: '', address: '', gateway: '' });
  uplink.wifi = {
    ...uplink.wifi,
    ssid: '',
    signalDbm: 0,
    bssid: undefined,
    channel: undefined,
    frequencyMhz: undefined,
  };
  demo.router.status.vpn = { ...demo.router.status.vpn, state: 'connecting', handshakeAgeSeconds: -1 };
}

const VPN_DOWN_WARNING =
  'The VPN tunnel is not working, and the kill switch is on: your devices have no internet until it works again.';
const VPN_DIRECT_WARNING =
  'The VPN tunnel is not working, and the kill switch is off: your devices are using the internet WITHOUT the VPN.';

/** Every state worth looking at, grouped as on the demo page. */
export const SCENARIOS: DemoScenario[] = [
  // Full-page notices
  { id: 'loading', group: 'Notices', title: 'Loading', route: '/', apply: (d) => void (d.phase = { kind: 'loading' }) },
  {
    id: 'unreachable',
    group: 'Notices',
    title: "Can't reach RasBeacon",
    route: '/',
    apply: (d) => void (d.phase = { kind: 'unreachable', error: 'Failed to fetch' }),
  },
  {
    id: 'setup-closed',
    group: 'Notices',
    title: 'Setup window closed',
    route: '/',
    apply: (d) => void (d.phase = { kind: 'notice', notice: 'setup-closed' }),
  },
  {
    id: 'setup-error',
    group: 'Notices',
    title: 'setup.txt problem',
    route: '/',
    apply: (d) => void (d.phase = { kind: 'notice', notice: 'setup-error' }),
  },
  {
    id: 'no-radio',
    group: 'Notices',
    title: 'No Wi-Fi adapter found',
    route: '/',
    apply: (d) => void (d.phase = { kind: 'notice', notice: 'no-radio' }),
  },

  // First-time setup and login
  {
    id: 'setup',
    group: 'Setup and login',
    title: 'First-time setup, open setup network',
    route: '/',
    apply: (d) => {
      d.phase = { kind: 'setup' };
      d.router.setup = { ...d.router.setup, mode: 'setup', claimed: false };
    },
  },
  {
    id: 'setup-secured',
    group: 'Setup and login',
    title: 'First-time setup, setup network with a password',
    route: '/',
    apply: (d) => {
      d.phase = { kind: 'setup' };
      d.router.setup = { ...d.router.setup, mode: 'setup', claimed: false, setupNetworkSecured: true };
    },
  },
  {
    id: 'login',
    group: 'Setup and login',
    title: 'Log in (the demo refuses the password "wrong")',
    route: '/',
    apply: (d) => void (d.phase = { kind: 'login' }),
  },

  // Status
  { id: 'all-good', group: 'Status', title: 'All good: VPN up, guests on', route: '/' },
  {
    id: 'status-loading',
    group: 'Status',
    title: 'Loading',
    route: '/',
    apply: (d) => void (d.loading.status = true),
  },
  {
    id: 'status-error',
    group: 'Status',
    title: "Couldn't load",
    route: '/',
    apply: (d) => {
      d.loading.status = true;
      d.errors.status = UNREACHABLE;
    },
  },
  {
    id: 'vpn-down',
    group: 'Status',
    title: 'VPN not working, kill switch on (no internet)',
    route: '/',
    apply: (d) => {
      d.router.status.vpn = { ...d.router.status.vpn, state: 'down', handshakeAgeSeconds: 600 };
      d.router.status.warnings = [VPN_DOWN_WARNING];
    },
  },
  {
    id: 'vpn-direct',
    group: 'Status',
    title: 'VPN not working, kill switch off (WITHOUT the VPN)',
    route: '/',
    apply: (d) => {
      d.router.status.vpn = { ...d.router.status.vpn, killSwitch: false, state: 'direct', handshakeAgeSeconds: 900 };
      d.router.vpnSettings.killSwitch = false;
      d.router.status.warnings = [VPN_DIRECT_WARNING];
    },
  },
  {
    id: 'vpn-connecting',
    group: 'Status',
    title: 'VPN connecting',
    route: '/',
    apply: (d) => {
      d.router.status.vpn = { ...d.router.status.vpn, state: 'connecting', handshakeAgeSeconds: -1 };
    },
  },
  {
    id: 'vpn-off',
    group: 'Status',
    title: 'VPN off, metered, public IPv6',
    route: '/',
    apply: (d) => {
      d.router.status.vpn = { ...d.router.status.vpn, enabled: false, state: 'off' };
      d.router.vpnSettings.enabled = false;
      d.router.status.hotspot.metered = true;
      d.router.hotspotSettings.metered = true;
      d.router.status.uplink.ipv6 = true;
    },
  },
  {
    id: 'portal-detected',
    group: 'Status',
    title: 'Hotel login page detected',
    route: '/',
    apply: (d) => detectLoginPage(d),
  },
  {
    id: 'portal-browsing-status',
    group: 'Status',
    title: "Login page open in RasBeacon's browser",
    route: '/',
    apply: (d) => openBrowser(d, true),
  },
  {
    id: 'ethernet',
    group: 'Status',
    title: 'Connected by Ethernet',
    route: '/',
    apply: (d) => {
      const { status } = d.router;
      status.uplink = {
        ...status.uplink,
        kind: 'ethernet',
        interface: 'eth0',
        address: '192.168.1.145',
        gateway: '192.168.1.1',
        subnetMask: '255.255.255.0',
        interfaceMac: '2c:cf:67:3f:8b:20',
        ipv6: true,
        wifi: { ...status.uplink.wifi, state: 'off', ssid: '', signalDbm: 0 },
        ethernet: { ...status.uplink.ethernet, cablePlugged: true },
      };
      status.ethernetDiagnostics = {
        carrierState: 'Link Detected (1 Gbps Full Duplex)',
        controller: 'eth0 (Realtek RTL8153 Gigabit)',
        mtu: '1500 bytes (Standard Ethernet)',
        packetStatistics: 'RX 1.48 GB / TX 820 MB (0 errors)',
        dnsServers: '192.168.1.1 (DHCP Supplied)',
        dhcpLease: '23h 48m remaining',
        portDescription: 'Port 1: IEEE 802.3ab 1000BASE-T Auto-Negotiated',
        pollingInterval: '30 sec',
      };
    },
  },
  {
    id: 'login-failed',
    group: 'Status',
    title: 'Wi-Fi login rejected',
    route: '/',
    apply: (d) => {
      disconnectUplink(d);
      d.router.status.uplink.wifi.state = 'login-failed';
    },
  },
  {
    id: 'offline-warnings',
    group: 'Status',
    title: 'Not connected, no devices, warnings, last refresh failed',
    route: '/',
    apply: (d) => {
      const { status } = d.router;
      disconnectUplink(d);
      status.uplink.wifi.state = 'searching';
      status.vpn = { ...status.vpn, enabled: false, state: 'off' };
      d.router.vpnSettings.enabled = false;
      status.clients = [];
      status.guest = { enabled: false, running: false, ssid: '', clients: [] };
      d.router.guestSettings.enabled = false;
      status.hotspot.metered = true;
      d.router.hotspotSettings.metered = true;
      status.warnings = [
        'No USB Wi-Fi adapter found, so the built-in Wi-Fi is running the hotspot. It can only hold about 8 devices and cannot also join an upstream network. Plug in a supported USB adapter and reboot.',
        'The upstream network uses the same addresses as your hotspot (192.168.8.0/24), so RasBeacon moved the hotspot to 192.168.9.0/24. Devices that were connected may need to reconnect.',
      ];
      d.errors.status = UNREACHABLE;
    },
  },
  {
    id: 'no-free-radio',
    group: 'Status',
    title: 'No Wi-Fi radio free for the internet, no Ethernet port',
    route: '/',
    apply: (d) => {
      const { status } = d.router;
      disconnectUplink(d);
      status.uplink.wifi = { ...status.uplink.wifi, available: false, state: 'off' };
      status.uplink.ethernet = { ...status.uplink.ethernet, present: false };
      status.radios = [{ ...status.radios[0], role: 'hotspot' }];
      d.router.uplinkSettings = { ...d.router.uplinkSettings, wifiUplinkAvailable: false, ethernetPresent: false };
      status.warnings = [
        'No USB Wi-Fi adapter found, so the built-in Wi-Fi is running the hotspot. It can only hold about 8 devices and cannot also join an upstream network. Plug in a supported USB adapter and reboot.',
      ];
    },
  },

  {
    id: 'zero2w',
    group: 'Status',
    title: 'Pi Zero 2 W: no Ethernet port, new versions are flashed',
    route: '/system',
    apply: (d) => {
      const { status } = d.router;
      status.uplink.ethernet = { ...status.uplink.ethernet, present: false };
      d.router.uplinkSettings = { ...d.router.uplinkSettings, ethernetPresent: false };
      status.update = { ...status.update, board: 'zero2w', canInstall: false, singleSystem: true };
      status.device = { ...status.device, modelSpec: 'Raspberry Pi Zero 2 W Rev 1.0' };
      status.storage = {
        ...status.storage,
        totalGb: 31.2,
        firmwareGb: 1.6,
        userGb: 2.1,
        medium: 'microSD card',
        filesystem: 'Writable system (ext4)',
      };
    },
  },

  // Errors
  {
    id: 'actions-fail',
    group: 'Errors',
    title: 'Every change fails (RasBeacon stopped answering)',
    route: '/',
    apply: (d) => void (d.failActions = true),
  },

  // Stress tests: the most and the least the router can send
  {
    id: 'worst-case',
    group: 'Stress test',
    title: 'Worst case: longest names, 16 devices, five warnings',
    route: '/',
    apply: applyWorstCase,
  },
  { id: 'no-clients', group: 'Stress test', title: 'No devices, no room', route: '/clients', apply: applyNoClients },
  { id: 'one-of-each', group: 'Stress test', title: 'One of everything', route: '/', apply: applyOneOfEach },

  // Internet
  {
    id: 'internet-no-scan',
    group: 'Internet',
    title: 'Before the first scan',
    route: '/internet',
    apply: (d) => void (d.nearbyNetworks = null),
  },
  {
    id: 'internet-nothing-found',
    group: 'Internet',
    title: 'Scan found nothing',
    route: '/internet',
    apply: (d) => void (d.nearbyNetworks = []),
  },
  {
    id: 'internet-none-saved',
    group: 'Internet',
    title: 'No networks saved',
    route: '/internet',
    apply: (d) => {
      disconnectUplink(d);
      d.router.status.uplink.wifi.state = 'not-configured';
      d.router.uplinkSettings.networks = [];
    },
  },
  {
    id: 'internet-searching',
    group: 'Internet',
    title: 'Looking for saved networks',
    route: '/internet',
    apply: (d) => {
      disconnectUplink(d);
      d.router.status.uplink.wifi.state = 'searching';
      // None of the saved networks is in range.
      const saved = new Set(d.router.uplinkSettings.networks.map((network) => network.ssid));
      d.nearbyNetworks = d.router.nearbyNetworks.filter((network) => !saved.has(network.ssid));
    },
  },
  {
    id: 'internet-connecting',
    group: 'Internet',
    title: 'Joining a saved network',
    route: '/internet',
    apply: (d) => {
      disconnectUplink(d);
      Object.assign(d.router.status.uplink.wifi, { state: 'connecting', ssid: 'GrandHyatt_Guest_5G', signalDbm: -61 });
    },
  },
  {
    id: 'internet-login-failed',
    group: 'Internet',
    title: 'Wi-Fi login rejected',
    route: '/internet',
    apply: (d) => {
      disconnectUplink(d);
      d.router.status.uplink.wifi.state = 'login-failed';
    },
  },
  {
    id: 'internet-client-port',
    group: 'Internet',
    title: 'Ethernet port as client port, with a DHCP server on it',
    route: '/internet',
    apply: (d) => {
      d.router.uplinkSettings.ethernetRole = 'client-port';
      d.router.status.uplink.ethernet = {
        ...d.router.status.uplink.ethernet,
        role: 'client-port',
        cablePlugged: true,
        foreignDhcpServer: '192.168.1.1',
      };
    },
  },
  {
    id: 'internet-no-radio',
    group: 'Internet',
    title: 'No Wi-Fi radio free, no Ethernet port',
    route: '/internet',
    apply: (d) => SCENARIOS.find((s) => s.id === 'no-free-radio')?.apply?.(d),
  },
  {
    id: 'internet-ethernet',
    group: 'Internet',
    title: 'Connected by Ethernet',
    route: '/internet',
    apply: (d) => SCENARIOS.find((s) => s.id === 'ethernet')?.apply?.(d),
  },
  {
    id: 'speedtest-never',
    group: 'Internet',
    title: 'Speed test never run',
    route: '/internet',
    apply: (d) => void (d.router.status.speedtest.last = null),
  },

  // VPN
  {
    id: 'vpn-not-set-up',
    group: 'VPN',
    title: 'Not set up yet',
    route: '/vpn',
    apply: (d) => {
      d.router.vpnSettings = { enabled: false, killSwitch: true, activeProfile: '', profiles: [] };
      d.router.status.vpn = {
        ...d.router.status.vpn,
        enabled: false,
        state: 'off',
        endpoint: '',
        handshakeAgeSeconds: -1,
        receivedBytes: 0,
        sentBytes: 0,
      };
    },
  },
  {
    id: 'vpn-page-down',
    group: 'VPN',
    title: 'Not working, kill switch on',
    route: '/vpn',
    apply: (d) => SCENARIOS.find((s) => s.id === 'vpn-down')?.apply?.(d),
  },
  {
    id: 'vpn-page-direct',
    group: 'VPN',
    title: 'Not working, kill switch off',
    route: '/vpn',
    apply: (d) => SCENARIOS.find((s) => s.id === 'vpn-direct')?.apply?.(d),
  },

  // Hotspot and guests
  { id: 'hotspot', group: 'Hotspot and guests', title: 'Hotspot settings', route: '/hotspot' },
  {
    id: 'guests-off',
    group: 'Hotspot and guests',
    title: 'Guest network off',
    route: '/guests',
    apply: (d) => {
      d.router.guestSettings.enabled = false;
      d.router.status.guest = { ...d.router.status.guest, enabled: false, running: false, clients: [] };
    },
  },
  {
    id: 'guests-first-time',
    group: 'Hotspot and guests',
    title: 'Guest network never set up (password needed)',
    route: '/guests',
    apply: (d) => {
      d.router.guestSettings = { enabled: false, ssid: 'RasBeacon-Guests', hasPassword: false, supported: true };
      d.router.status.guest = { enabled: false, running: false, ssid: '', clients: [] };
    },
  },
  {
    id: 'guests-unsupported',
    group: 'Hotspot and guests',
    title: "Adapter can't run a guest network",
    route: '/guests',
    apply: (d) => {
      d.router.guestSettings = { ...d.router.guestSettings, enabled: false, supported: false };
      d.router.status.guest = { enabled: false, running: false, ssid: '', clients: [] };
    },
  },

  // SSH and HTTPS
  {
    id: 'ssh-off',
    group: 'SSH and HTTPS',
    title: 'SSH off, no keys',
    route: '/ssh',
    apply: (d) => void (d.router.sshSettings = { enabled: false, authorizedKeys: '' }),
  },
  {
    id: 'https-off',
    group: 'SSH and HTTPS',
    title: 'HTTPS off',
    route: '/https',
    apply: (d) => void (d.router.httpsSettings.enabled = false),
  },
  {
    id: 'https-none',
    group: 'SSH and HTTPS',
    title: 'HTTPS off, no certificate yet',
    route: '/https',
    apply: (d) =>
      void (d.router.httpsSettings = { enabled: false, uploaded: false, subject: '', fingerprint: '', validUntil: '' }),
  },
  {
    id: 'https-uploaded',
    group: 'SSH and HTTPS',
    title: 'Your own certificate',
    route: '/https',
    apply: (d) => {
      d.router.httpsSettings = {
        enabled: true,
        uploaded: true,
        subject: 'CN=router.example.net',
        fingerprint: 'A1:07:5C:93:E2:4B:18:F0:6D:2A:B5:C8:39:71:0E:D4:56:9B:22:E7:8F:13:AC:60:4D:F9:05:B1:7A:3E:C2:88',
        validUntil: '2027-03-14',
      };
    },
  },

  // System
  { id: 'system', group: 'System', title: 'Up to date', route: '/system' },
  {
    id: 'update-available',
    group: 'System',
    title: 'New version available',
    route: '/system',
    apply: (d) => {
      Object.assign(d.router.status.update, {
        checkOnline: true,
        latestVersion: 'v1.1.0',
        latestUrl: 'https://github.com/rasbeacon/rasbeacon/releases/tag/v1.1.0',
      });
    },
  },
  {
    id: 'update-installing',
    group: 'System',
    title: 'Installing an update',
    route: '/system',
    apply: (d) => void (d.router.status.update.installing = true),
  },
  {
    id: 'update-result',
    group: 'System',
    title: 'Last update failed',
    route: '/system',
    apply: (d) => {
      d.router.status.update.message = 'This file is not signed by a key this RasBeacon trusts. Nothing was changed.';
    },
  },
  {
    id: 'update-unsupported',
    group: 'System',
    title: "This image can't install updates",
    route: '/system',
    apply: (d) => Object.assign(d.router.status.update, { canInstall: false, canCheckOnline: false }),
  },

  // The hotel login page screen, as phones open it
  {
    id: 'portal-loading',
    group: 'Hotel login page',
    title: 'Loading',
    route: '/portal',
    apply: (d) => void (d.loading.portal = true),
  },
  {
    id: 'portal-error',
    group: 'Hotel login page',
    title: "Couldn't load",
    route: '/portal',
    apply: (d) => {
      d.loading.portal = true;
      d.errors.portal = UNREACHABLE;
    },
  },
  {
    id: 'portal-both',
    group: 'Hotel login page',
    title: 'Both ways offered',
    route: '/portal',
    apply: (d) => detectLoginPage(d),
  },
  {
    id: 'portal-browse-only',
    group: 'Hotel login page',
    title: 'Only the zero-leak login',
    route: '/portal',
    apply: (d) => detectLoginPage(d, { canStart: false }),
  },
  {
    id: 'portal-login-mode-only',
    group: 'Hotel login page',
    title: 'Only login mode',
    route: '/portal',
    apply: (d) => detectLoginPage(d, { canBrowse: false }),
  },
  {
    id: 'portal-picture-loading',
    group: 'Hotel login page',
    title: 'Browser open, picture loading',
    route: '/portal',
    apply: (d) => openBrowser(d, false),
  },
  {
    id: 'portal-browser',
    group: 'Hotel login page',
    title: 'Browser open, login page shown',
    route: '/portal',
    apply: (d) => openBrowser(d, true),
  },
  {
    id: 'portal-browser-error',
    group: 'Hotel login page',
    title: 'Browser error',
    route: '/portal',
    apply: (d) => {
      openBrowser(d, false);
      d.portalBrowser = { ...d.portalBrowser, error: "The browser didn't respond: context deadline exceeded" };
    },
  },
  {
    id: 'portal-login-mode',
    group: 'Hotel login page',
    title: 'Login mode on (countdown)',
    route: '/portal',
    apply: (d) => detectLoginPage(d, { loginMode: true, secondsLeft: 512 }),
  },
  {
    id: 'portal-reachable',
    group: 'Hotel login page',
    title: 'Nothing to do: devices can reach the page',
    route: '/portal',
    apply: (d) => detectLoginPage(d, { canStart: false, canBrowse: false }),
  },
  { id: 'portal-none', group: 'Hotel login page', title: 'Nothing to do: no login page', route: '/portal' },
];
