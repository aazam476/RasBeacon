import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useLocation } from 'react-router';
import { FALLBACK_LOGIN_URL } from '../constants';
import {
  DeviceContext,
  type DataKey,
  type Device,
  type DeviceActions,
  type DeviceState,
  type Loadable,
  type PortalBrowserState,
} from '../device';
import { FieldErrors } from '../errors';
import type {
  DNSSettings,
  GuestSettings,
  HotspotSettings,
  HTTPSSettings,
  NearbyNetwork,
  PortalStatus,
  SetupState,
  SSHSettings,
  SwapSettings,
  Status,
  UplinkSettings,
  UsageSettings,
  VPNSettings,
} from '../types';
import * as api from './api';
import { ApiError } from './api';
import {
  checkGuestForm,
  checkNewNetwork,
  checkSetupForm,
  classifyGesture,
  decidePhase,
  pageSizeFor,
  sameNames,
  screenError,
  splitList,
  toSavedNetwork,
  vpnDetailsRequest,
} from './logic';

/** What each piece of data holds. */
interface DataTypes {
  status: Status;
  hotspotSettings: HotspotSettings;
  guestSettings: GuestSettings;
  uplinkSettings: UplinkSettings;
  vpnSettings: VPNSettings;
  sshSettings: SSHSettings;
  dnsSettings: DNSSettings;
  swapSettings: SwapSettings;
  usageSettings: UsageSettings;
  httpsSettings: HTTPSSettings;
  portal: PortalStatus;
}

type AllData = { [K in DataKey]: Loadable<DataTypes[K]> };

const LOADERS: { [K in DataKey]: () => Promise<DataTypes[K]> } = {
  status: api.getStatus,
  hotspotSettings: api.getHotspotSettings,
  guestSettings: api.getGuestSettings,
  uplinkSettings: api.getUplinkSettings,
  vpnSettings: api.getVPNSettings,
  sshSettings: api.getSSHSettings,
  dnsSettings: api.getDNSSettings,
  swapSettings: api.getSwapSettings,
  usageSettings: api.getUsageSettings,
  httpsSettings: api.getHTTPSSettings,
  portal: api.getPortalStatus,
};

const NOTHING = { data: undefined, error: null };
const NO_DATA: AllData = {
  status: NOTHING,
  hotspotSettings: NOTHING,
  guestSettings: NOTHING,
  uplinkSettings: NOTHING,
  vpnSettings: NOTHING,
  sshSettings: NOTHING,
  dnsSettings: NOTHING,
  swapSettings: NOTHING,
  usageSettings: NOTHING,
  httpsSettings: NOTHING,
  portal: NOTHING,
};

/** The settings loaded once logged in, and again when their page opens. */
const SETTINGS: DataKey[] = [
  'hotspotSettings',
  'guestSettings',
  'uplinkSettings',
  'vpnSettings',
  'sshSettings',
  'dnsSettings',
  'swapSettings',
  'usageSettings',
  'httpsSettings',
];

/** What each page loads again when it opens. */
const PAGE_DATA: Record<string, DataKey[]> = {
  '/': ['status'],
  '/internet': ['uplinkSettings', 'dnsSettings', 'status'],
  '/vpn': ['vpnSettings', 'status'],
  '/hotspot': ['hotspotSettings'],
  '/guests': ['guestSettings'],
  '/clients': ['status', 'usageSettings'],
  '/ssh': ['sshSettings'],
  '/https': ['httpsSettings'],
  '/system': ['status', 'swapSettings'],
  '/more': ['sshSettings', 'httpsSettings'],
};

const STATUS_EVERY_MS = 10_000;
const PORTAL_EVERY_MS = 5_000;

const CLOSED_BROWSER: PortalBrowserState = { frame: undefined, imageSrc: '', error: null };

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * The real router, through its API (api.ts). It implements every action in device.ts as
 * README.md ("Connecting the real router") describes, with the logic of the original UI
 * (logic.ts).
 */
export function RealDeviceProvider({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const [setup, setSetup] = useState<SetupState | undefined>(undefined);
  const [loggedIn, setLoggedIn] = useState<boolean | undefined>(undefined);
  const [startupError, setStartupError] = useState<string | null>(null);
  const [data, setData] = useState<AllData>(NO_DATA);
  const [nearbyNetworks, setNearbyNetworks] = useState<NearbyNetwork[] | null>(null);
  const [portalBrowser, setPortalBrowser] = useState<PortalBrowserState>(CLOSED_BROWSER);

  const phase = useMemo(() => decidePhase(setup, loggedIn, startupError), [setup, loggedIn, startupError]);
  const ready = phase.kind === 'ready';
  const setupDone = phase.kind === 'ready' || phase.kind === 'login';

  // The newest data, for actions that send it back with one change. Updated after each render.
  const latest = useRef(data);
  useEffect(() => {
    latest.current = data;
  }, [data]);
  // Where the last finger or mouse went down on the login page picture.
  const pointerDownAt = useRef<{ clientX: number; clientY: number } | null>(null);
  const frameRef = useRef(portalBrowser.frame);
  useEffect(() => {
    frameRef.current = portalBrowser.frame;
  }, [portalBrowser.frame]);

  /** Stores an answer of the router. */
  const store = useCallback(<K extends DataKey>(key: K, value: DataTypes[K]) => {
    setData((current) => ({ ...current, [key]: { data: value, error: null } }));
  }, []);

  /** A request answered 401: the session ended (logged out elsewhere, or RasBeacon restarted). */
  const noticeLogout = useCallback((error: unknown) => {
    if (error instanceof ApiError && error.status === 401) setLoggedIn(false);
  }, []);

  /** Loads one piece of data. It never fails: a problem goes into the data's `error`. */
  const load = useCallback(
    async (key: DataKey) => {
      try {
        const value = await LOADERS[key]();
        setData((current) => ({ ...current, [key]: { data: value, error: null } }));
      } catch (error) {
        noticeLogout(error);
        setData((current) => ({ ...current, [key]: { ...current[key], error: messageOf(error) } }));
      }
    },
    [noticeLogout],
  );

  /** Loads the setup state and the session (what the phase is decided from). */
  const loadStartup = useCallback(async () => {
    try {
      const [state, session] = await Promise.all([api.getSetupState(), api.getSession()]);
      setSetup(state);
      setLoggedIn(session.loggedIn);
      setStartupError(null);
    } catch (error) {
      setStartupError(messageOf(error));
    }
  }, []);

  // --- Loading and refreshing ------------------------------------------------------------------

  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect -- the state changes when the router answers
    void loadStartup();
  }, [loadStartup]);

  // Logged in: load everything, then the status every 10 seconds.
  useEffect(() => {
    if (!ready) return;
    // oxlint-disable-next-line react/set-state-in-effect -- the state changes when the router answers
    void load('status');
    // oxlint-disable-next-line react/set-state-in-effect -- the state changes when the router answers
    for (const key of SETTINGS) void load(key);
    const timer = setInterval(() => void load('status'), STATUS_EVERY_MS);
    return () => clearInterval(timer);
  }, [ready, load]);

  // A page loads its data again when it opens.
  useEffect(() => {
    if (!ready) return;
    // oxlint-disable-next-line react/set-state-in-effect -- the state changes when the router answers
    for (const key of PAGE_DATA[pathname] ?? []) void load(key);
  }, [ready, pathname, load]);

  // The login page screen refreshes the portal status every 5 seconds, logged in or not.
  const onPortalScreen = pathname === '/portal' && setupDone;
  useEffect(() => {
    if (!onPortalScreen) return;
    // oxlint-disable-next-line react/set-state-in-effect -- the state changes when the router answers
    void load('portal');
    const timer = setInterval(() => void load('portal'), PORTAL_EVERY_MS);
    return () => clearInterval(timer);
  }, [onPortalScreen, load]);

  // While RasBeacon's browser is open, wait for each new picture of the login page.
  const browsing = data.portal.data?.browsing === true;
  useEffect(() => {
    if (!browsing) return;
    let open = true;
    void (async () => {
      let seq = 0;
      while (open) {
        try {
          const frame = await api.getPortalBrowserFrame(seq);
          if (!open) return;
          if (!frame.running) {
            setPortalBrowser(CLOSED_BROWSER);
            void load('portal');
            return;
          }
          seq = frame.seq;
          const { image, ...details } = frame;
          setPortalBrowser((current) => ({
            frame: details,
            imageSrc: image ? `data:image/jpeg;base64,${image}` : current.imageSrc,
            error: null,
          }));
        } catch (error) {
          if (!open) return;
          setPortalBrowser((current) => ({ ...current, error: messageOf(error) }));
          await sleep(2000);
        }
      }
    })();
    return () => {
      open = false;
    };
  }, [browsing, load]);

  // --- Actions ---------------------------------------------------------------------------------

  const actions = useMemo<DeviceActions>(() => {
    /**
     * Runs a request for an action: a 401 means the session ended, and a refusal becomes what
     * the screen shows (`fields` maps the router's field names to the screen's keys).
     */
    async function attempt<T>(request: () => Promise<T>, fields?: Record<string, string>): Promise<T> {
      try {
        return await request();
      } catch (error) {
        noticeLogout(error);
        throw screenError(error, fields);
      }
    }

    /** Sends input to RasBeacon's browser; problems go to `portalBrowser.error`. */
    function sendInput(input: api.BrowserInput) {
      api
        .sendPortalBrowserInput(input)
        .catch((error: unknown) => setPortalBrowser((current) => ({ ...current, error: messageOf(error) })));
    }

    /** After an action that changes the status, show it. */
    const refreshStatus = () => load('status');

    function patchStatus(change: (status: Status) => Status) {
      setData((current) =>
        current.status.data
          ? { ...current, status: { ...current.status, data: change(current.status.data) } }
          : current,
      );
    }

    /** Waits until RasBeacon answers again after restarting. */
    async function waitForRestart() {
      const started = Date.now();
      let wentAway = false;
      await sleep(5000);
      while (Date.now() - started < 5 * 60_000) {
        try {
          await api.getSetupState();
          // Back (or it never went away: then give it a minute and a half).
          if (wentAway || Date.now() - started > 90_000) return;
        } catch {
          wentAway = true;
        }
        await sleep(3000);
      }
      throw new Error(
        "RasBeacon didn't answer after restarting. Check that this device is connected to its Wi-Fi, then reload the page.",
      );
    }

    const vpnSwitches = () => {
      const vpn = latest.current.vpnSettings.data;
      return { enabled: vpn?.enabled ?? false, killSwitch: vpn?.killSwitch ?? true };
    };

    async function vpnChange(request: () => Promise<VPNSettings>, fields?: Record<string, string>) {
      store('vpnSettings', await attempt(request, fields));
      await refreshStatus();
    }

    async function httpsChange(change: Parameters<typeof api.putHTTPSSettings>[0]) {
      store('httpsSettings', await attempt(() => api.putHTTPSSettings(change)));
    }

    async function uplinkChange(settings: UplinkSettings) {
      store('uplinkSettings', await attempt(() => api.putUplinkSettings(settings)));
      await refreshStatus();
    }

    async function portalChange(request: () => Promise<PortalStatus>) {
      const portal = await attempt(request);
      store('portal', portal);
      return portal;
    }

    return {
      retry: loadStartup,
      reload: load,

      finishSetup: async (form) => {
        const problems = checkSetupForm(form);
        if (Object.keys(problems).length > 0) throw new FieldErrors(problems);
        // The phase stays `setup`: the screen shows "Setup complete" from the answer.
        return attempt(
          () =>
            api.claim({
              adminPassword: form.adminPassword,
              hotspotSsid: form.hotspotSsid,
              hotspotPassword: form.hotspotPassword,
              hotspotBand: form.hotspotBand,
            }),
          {
            ...sameNames('adminPassword', 'hotspotSsid', 'hotspotPassword'),
            'hotspot.ssid': 'hotspotSsid',
          },
        );
      },
      logIn: async (password, stayLoggedIn) => {
        await attempt(() => api.logIn(password, stayLoggedIn));
        setLoggedIn(true);
      },
      logOut: async () => {
        await attempt(api.logOut);
        setLoggedIn(false);
        // The next person to log in sees nothing of this session.
        setData((current) => ({ ...NO_DATA, portal: current.portal }));
        setNearbyNetworks(null);
      },

      checkPortal: async () => {
        await portalChange(() => api.portalAction('check'));
        // The check takes a few seconds on the router.
        setTimeout(() => void load('portal'), 6000);
      },
      startLoginMode: async () => {
        const portal = await portalChange(() => api.portalAction('start'));
        // As the original: then go to the login page in this window.
        setTimeout(() => window.location.assign(portal.loginUrl || FALLBACK_LOGIN_URL), 2000);
      },
      extendLoginMode: async () => void (await portalChange(() => api.portalAction('extend'))),
      stopLoginMode: async () => void (await portalChange(() => api.portalAction('stop'))),
      openPortalBrowser: async () => {
        setPortalBrowser(CLOSED_BROWSER);
        await portalChange(() => api.startPortalBrowser(pageSizeFor(window.innerWidth, window.innerHeight)));
      },
      closePortalBrowser: async () => {
        await portalChange(api.stopPortalBrowser);
        setPortalBrowser(CLOSED_BROWSER);
      },
      portalPointerDown: (spot) => {
        pointerDownAt.current = spot;
      },
      portalPointerUp: (spot, box) => {
        const start = pointerDownAt.current;
        pointerDownAt.current = null;
        const frame = frameRef.current;
        if (start && frame) sendInput(classifyGesture(start, spot, box, frame));
      },
      portalWheel: (spot, box, deltaY) => {
        const frame = frameRef.current;
        if (!frame) return;
        const at = classifyGesture(spot, spot, box, frame);
        if (at.action === 'click') sendInput({ action: 'scroll', x: at.x, y: at.y, deltaY: Math.round(deltaY) });
      },
      portalType: (text) => {
        if (text !== '') sendInput({ action: 'type', text });
      },
      portalKey: (key) => sendInput({ action: 'key', key }),
      portalBack: () => sendInput({ action: 'back' }),
      portalReload: () => sendInput({ action: 'reload' }),

      refreshStatus,

      setEthernetRole: async (role) => {
        const saved = latest.current.uplinkSettings.data;
        if (!saved) throw new Error("The Internet settings haven't loaded yet.");
        await uplinkChange({ ...saved, ethernetRole: role });
      },
      scanNearbyNetworks: async () => {
        const { networks } = await attempt(api.scanNearbyNetworks);
        setNearbyNetworks(networks);
      },
      prepareNetwork: async (form, list) => {
        const problems = checkNewNetwork(form, list);
        if (Object.keys(problems).length > 0) throw new FieldErrors(problems);
        return toSavedNetwork(form);
      },
      saveWifiNetworks: async (networks) => {
        const saved = latest.current.uplinkSettings.data;
        if (!saved) throw new Error("The Internet settings haven't loaded yet.");
        await uplinkChange({ ...saved, networks });
      },
      disconnectUplink: async () => {
        await attempt(() => api.uplinkAction('disconnect'));
        await refreshStatus();
      },
      renewDhcpLease: async () => {
        await attempt(() => api.uplinkAction('renew'));
        await refreshStatus();
      },
      rescanEthernetPort: async () => {
        await attempt(() => api.uplinkAction('ethernet/scan'));
        await refreshStatus();
      },
      saveDnsSettings: async (servers, withVpn) => {
        const saved = await attempt(
          () => api.putDNSSettings({ servers: splitList(servers), withVpn }),
          sameNames('servers', 'withVpn'),
        );
        store('dnsSettings', saved);
      },
      runSpeedtest: async () => {
        const speedtest = await attempt(api.runSpeedtest);
        patchStatus((status) => ({ ...status, speedtest }));
      },

      setVpnEnabled: (enabled) => vpnChange(() => api.putVPNSwitches({ ...vpnSwitches(), enabled })),
      setKillSwitch: (killSwitch) => vpnChange(() => api.putVPNSwitches({ ...vpnSwitches(), killSwitch })),
      setActiveVpnProfile: (name) => vpnChange(() => api.vpnProfileAction('activate', name)),
      removeVpnProfile: (name) => vpnChange(() => api.vpnProfileAction('remove', name)),
      importVpnProfile: ({ text, fileName }) => vpnChange(() => api.importVPNProfile(text, fileName)),
      saveVpnDetails: async (profile, form) => {
        const details = vpnDetailsRequest(profile, form);
        await vpnChange(
          () => api.saveVPNProfile(details),
          sameNames(
            'endpoint',
            'peerPublicKey',
            'addresses',
            'dns',
            'allowedIps',
            'persistentKeepalive',
            'mtu',
            'routedIpv6Prefix',
            'presharedKey',
            'privateKey',
          ),
        );
      },
      generateVpnKey: (profile) => vpnChange(() => api.vpnProfileAction('generate-key', profile)),

      saveHotspotSettings: async (settings) => {
        const saved = await attempt(() => api.putHotspotSettings(settings), {
          ...sameNames('ssid', 'password', 'channel', 'country'),
          'hotspot.ssid': 'ssid',
          'hotspot.channel': 'channel',
          'hotspot.country': 'country',
          hotspotPassword: 'password',
        });
        store('hotspotSettings', saved);
        await refreshStatus();
      },
      revealHotspotPassword: async (adminPassword) => {
        const { hotspotPassword } = await attempt(() => api.revealHotspotPassword(adminPassword));
        return hotspotPassword;
      },
      wifiQr: (network, adminPassword) => attempt(() => api.wifiQr(network, adminPassword)),
      saveGuestSettings: async (settings) => {
        const problems = checkGuestForm(settings);
        if (Object.keys(problems).length > 0) throw new FieldErrors(problems);
        const saved = await attempt(() => api.putGuestSettings(settings), {
          ...sameNames('enabled', 'ssid', 'password'),
          'hotspot.ssid': 'ssid',
        });
        store('guestSettings', saved);
        await refreshStatus();
      },

      setClientInternetAccess: async (mac, allowed) => {
        await attempt(() => api.setClientInternetAccess(mac, allowed));
        await refreshStatus();
      },
      disconnectClient: async (mac) => {
        await attempt(() => api.clientAction('disconnect', mac));
        await refreshStatus();
      },
      flushClientLease: async (mac) => {
        await attempt(() => api.clientAction('flush-lease', mac));
        await refreshStatus();
      },
      renameClient: async (mac, name) => {
        await attempt(() => api.renameClient(mac, name), sameNames('name'));
        await refreshStatus();
      },
      saveUsageLimit: async (limitMb) => {
        store('usageSettings', await attempt(() => api.putUsageSettings(limitMb), sameNames('limitMb')));
        await refreshStatus();
      },
      resetUsage: async () => {
        store('usageSettings', await attempt(() => api.resetUsage()));
        await refreshStatus();
      },

      saveSshSettings: async (settings) => {
        store('sshSettings', await attempt(() => api.putSSHSettings(settings)));
      },
      setHttpsEnabled: (enabled) => httpsChange({ enabled }),
      makeNewCertificate: () =>
        httpsChange({ enabled: latest.current.httpsSettings.data?.enabled ?? false, generateNew: true }),
      uploadCertificate: async ({ certificate, privateKey }) => {
        const [certificateText, keyText] = await Promise.all([certificate.text(), privateKey.text()]);
        await httpsChange({
          enabled: latest.current.httpsSettings.data?.enabled ?? false,
          certificate: certificateText,
          privateKey: keyText,
        });
      },

      installUpdate: async (file) => {
        const update = await attempt(() => api.installUpdate(file));
        patchStatus((status) => ({ ...status, update }));
      },
      saveSwapSettings: async (sizeGb) => {
        store('swapSettings', await attempt(() => api.putSwapSettings(sizeGb), sameNames('sizeGb')));
        await refreshStatus();
      },
      setCheckOnline: async (enabled) => {
        const update = await attempt(() => api.setCheckOnline(enabled));
        patchStatus((status) => ({ ...status, update }));
      },
      reboot: async () => {
        await attempt(api.reboot);
        await waitForRestart();
        // The session may have ended with the restart ("Stay logged in" keeps it).
        await loadStartup();
      },
      factoryReset: async (adminPassword) => {
        await attempt(() => api.factoryReset(adminPassword));
        setData(NO_DATA);
        setNearbyNetworks(null);
        setLoggedIn(false);
        // RasBeacon restarts into setup with an open setup network: show "loading" until it
        // answers again (then the phase is `setup`).
        setSetup(undefined);
        void (async () => {
          for (let tries = 0; tries < 100; tries++) {
            await sleep(3000);
            try {
              const state = await api.getSetupState();
              if (state.mode !== 'normal') {
                setSetup(state);
                setLoggedIn(false);
                return;
              }
            } catch {
              // Still restarting.
            }
          }
          setStartupError(api.UNREACHABLE);
        })();
      },
    };
  }, [load, loadStartup, noticeLogout, store]);

  const state = useMemo<DeviceState>(
    () => ({
      phase,
      setup,
      ...data,
      nearbyNetworks,
      portalBrowser,
      diagnosticsUrl: api.DIAGNOSTICS_URL,
    }),
    [phase, setup, data, nearbyNetworks, portalBrowser],
  );

  const value = useMemo<Device>(() => ({ ...state, ...actions }), [state, actions]);
  return <DeviceContext value={value}>{children}</DeviceContext>;
}
