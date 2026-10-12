import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { useLocation } from 'react-router';
import {
  DeviceContext,
  type DataKey,
  type Device,
  type DeviceActions,
  type DeviceState,
  type Loadable,
} from '../device';
import { FieldErrors } from '../errors';
import type { SavedNetwork, VPNProfile } from '../types';
import { DemoIndex } from './DemoIndex';
import { DemoTag } from './DemoTag';
import { StressToggle } from './StressToggle';
import { LOGIN_PAGE_PICTURE } from './loginPagePicture';
import {
  checkGuestSettings,
  checkHotspotSettings,
  checkNewNetwork,
  checkSetupForm,
  checkWifiPassword,
  checkWireGuardFile,
} from './rules';
import { CLOSED_BROWSER, createDemoState, SCENARIOS, UNREACHABLE, type DemoState } from './scenarios';

/** Waits a moment, so the screens' busy states can be seen while there is no real router. */
function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Actions that keep working when every change fails: loading, and input on the login page picture. */
const ALWAYS_WORKING = new Set<string>([
  'retry',
  'reload',
  'refreshStatus',
  'portalPointerDown',
  'portalPointerUp',
  'portalWheel',
  'portalType',
  'portalKey',
  'portalBack',
  'portalReload',
] satisfies (keyof DeviceActions)[]);

/** The same actions, except that each one that changes something fails after a moment. */
function failingActions<T extends object>(actions: T): T {
  const failing: Record<string, unknown> = { ...(actions as Record<string, unknown>) };
  for (const name of Object.keys(actions)) {
    if (ALWAYS_WORKING.has(name)) continue;
    failing[name] = async () => {
      await wait(700);
      throw new Error(UNREACHABLE);
    };
  }
  return failing as T;
}

/** The scenario named in the address (`?scenario=vpn-down#/`), applied to a fresh demo router. */
function initialState(): DemoState {
  const state = createDemoState();
  const id = new URLSearchParams(window.location.search).get('scenario');
  SCENARIOS.find((scenario) => scenario.id === id)?.apply?.(state);
  return state;
}

/** A plausible-looking WireGuard key, so new keys visibly change in the demo. */
function randomKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  return btoa(String.fromCharCode(...bytes));
}

function randomFingerprint(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(32)), (byte) =>
    byte.toString(16).padStart(2, '0').toUpperCase(),
  ).join(':');
}

/** Reads the [Interface] and [Peer] values a profile shows from a WireGuard file. */
function profileFromFile(name: string, text: string): VPNProfile {
  const value = (key: string) => new RegExp(`^\\s*${key}\\s*=\\s*(.+)$`, 'im').exec(text)?.[1].trim() ?? '';
  const list = (key: string) =>
    value(key)
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  return {
    name,
    addresses: list('Address'),
    dns: list('DNS'),
    mtu: Number(value('MTU')) || 0,
    peerPublicKey: value('PublicKey'),
    endpoint: value('Endpoint'),
    allowedIps: list('AllowedIPs'),
    persistentKeepalive: Number(value('PersistentKeepalive')) || 0,
    publicKey: randomKey(),
    hasPresharedKey: value('PresharedKey') !== '',
    routedIpv6Prefix: '',
  };
}

/**
 * Demo implementation of the router: it serves the sample router from mockData.ts, changed by
 * the scenario in the address, and keeps changes in memory. It also adds the demo page
 * (`/#/demo`, every scenario) and the DEMO tag that links to it.
 *
 * To connect the real router, write a provider with the same `DeviceContext` value (see
 * README.md) and use it in App.tsx instead of this one.
 */
export function DemoDeviceProvider({ children }: { children: ReactNode }) {
  const [demo, setDemo] = useState(initialState);
  const { pathname } = useLocation();
  const { failActions } = demo;

  // Every action but one, made once: they change the state with `setDemo`.
  const actions = useMemo<Omit<DeviceActions, 'revealHotspotPassword'>>(() => {
    /** Applies a change to a copy of the demo state. */
    function change(mutate: (draft: DemoState) => void) {
      setDemo((current) => {
        const draft = structuredClone(current);
        mutate(draft);
        return draft;
      });
    }

    function failIfAny(errors: Record<string, string>) {
      if (Object.keys(errors).length > 0) throw new FieldErrors(errors);
    }

    function loginSucceeded() {
      change((d) => {
        const { status } = d.router;
        status.portal = {
          ...status.portal,
          detected: false,
          canStart: false,
          canBrowse: false,
          loginMode: false,
          secondsLeft: 0,
          browsing: false,
        };
        status.vpn = { ...status.vpn, state: status.vpn.enabled ? 'up' : 'off', handshakeAgeSeconds: 2 };
        d.portalBrowser = CLOSED_BROWSER;
      });
    }

    const all: Omit<DeviceActions, 'revealHotspotPassword'> = {
      retry: async () => {
        await wait(900);
        change((d) => {
          if (d.phase.kind === 'unreachable' || d.phase.kind === 'loading') d.phase = { kind: 'ready' };
        });
      },
      reload: async (what: DataKey) => {
        await wait(700);
        change((d) => {
          delete d.loading[what];
          delete d.errors[what];
        });
      },

      finishSetup: async (form) => {
        await wait(1200);
        failIfAny(checkSetupForm(form));
        change((d) => {
          d.router.setup = { ...d.router.setup, mode: 'normal', claimed: true };
          Object.assign(d.router.hotspotSettings, { ssid: form.hotspotSsid, band: form.hotspotBand, channel: 0 });
          Object.assign(d.router.status.hotspot, { ssid: form.hotspotSsid, band: form.hotspotBand });
          d.router.hotspotPassword = form.hotspotPassword;
        });
        return { hotspotSsid: form.hotspotSsid, switchInSeconds: 8, adminUrl: 'http://192.168.8.1/' };
      },
      logIn: async (password) => {
        await wait(900);
        if (password === '' || password === 'wrong') throw new Error('Wrong password.');
        change((d) => void (d.phase = { kind: 'ready' }));
      },
      logOut: async () => {
        await wait(400);
        change((d) => void (d.phase = { kind: 'login' }));
      },

      checkPortal: () => wait(1500),
      startLoginMode: async () => {
        await wait(900);
        change((d) => Object.assign(d.router.status.portal, { loginMode: true, secondsLeft: 600 }));
      },
      extendLoginMode: async () => {
        await wait(600);
        change((d) => void (d.router.status.portal.secondsLeft += 600));
      },
      stopLoginMode: async () => {
        await wait(600);
        change((d) => Object.assign(d.router.status.portal, { loginMode: false, secondsLeft: 0 }));
      },
      openPortalBrowser: async () => {
        await wait(1500);
        change((d) => {
          d.router.status.portal.browsing = true;
          d.portalBrowser = CLOSED_BROWSER;
        });
        // The first picture arrives a moment later.
        setTimeout(
          () =>
            change((d) => {
              if (!d.router.status.portal.browsing) return;
              d.portalBrowser = {
                frame: {
                  running: true,
                  seq: 1,
                  width: 390,
                  height: 591,
                  url: d.router.status.portal.loginUrl ?? '',
                  title: 'Guest Wi-Fi Login',
                },
                imageSrc: LOGIN_PAGE_PICTURE,
                error: null,
              };
            }),
          1200,
        );
      },
      closePortalBrowser: async () => {
        await wait(500);
        change((d) => {
          d.router.status.portal.browsing = false;
          d.portalBrowser = CLOSED_BROWSER;
        });
      },
      // Taps, swipes and typing on the demo's picture change nothing (the real provider sends them).
      portalPointerDown: () => {},
      portalPointerUp: () => {},
      portalWheel: () => {},
      portalType: () => {},
      // In the demo, pressing Enter on the page logs in: the browser closes by itself.
      portalKey: (key) => {
        if (key === 'Enter') setTimeout(loginSucceeded, 1500);
      },
      portalBack: () => {},
      portalReload: () => {},

      refreshStatus: async () => {
        await wait(800);
        change((d) => {
          delete d.loading.status;
          delete d.errors.status;
        });
      },

      setEthernetRole: async (role) => {
        await wait(1200);
        change((d) => {
          d.router.uplinkSettings.ethernetRole = role;
          d.router.status.uplink.ethernet = { ...d.router.status.uplink.ethernet, role, foreignDhcpServer: undefined };
        });
      },
      scanNearbyNetworks: async () => {
        await wait(2500);
        change((d) => void (d.nearbyNetworks = d.router.nearbyNetworks));
      },
      prepareNetwork: async (form, list) => {
        await wait(300);
        failIfAny(checkNewNetwork(form, list));
        const network: SavedNetwork = {
          id: '',
          ssid: form.ssid,
          security: form.security,
          hidden: form.hidden,
          hasPassword: false,
          password: form.security === 'open' ? '' : form.password,
        };
        if (form.security === 'enterprise') {
          const tls = form.method === 'tls';
          network.enterprise = {
            method: form.method,
            identity: form.identity,
            anonymousIdentity: form.anonymousIdentity,
            serverName: form.serverName.trim(),
            allowNoCertificateCheck: form.allowNoCertificateCheck,
            caCertSummary: form.caCertFiles.map((file) => file.name).join(', '),
            clientCertSummary: tls ? (form.clientCertFile?.name ?? '') : '',
            hasClientKey: tls && form.clientKeyFile !== null,
          };
        }
        return network;
      },
      saveWifiNetworks: async (networks) => {
        await wait(1400);
        const errors: Record<string, string> = {};
        networks.forEach((network, index) => {
          if (network.security === 'personal' && network.password) {
            const problem = checkWifiPassword(network.password);
            if (problem) errors[`networks.${index}`] = problem;
          }
        });
        failIfAny(errors);
        change((d) => {
          d.router.uplinkSettings.networks = networks.map((network, index) => ({
            ...network,
            id: network.id || `saved-${Date.now()}-${index}`,
            hasPassword: network.hasPassword || Boolean(network.password),
            password: undefined,
          }));
        });
      },
      disconnectUplink: async () => {
        await wait(1200);
        change((d) => {
          const { uplink } = d.router.status;
          Object.assign(uplink, { connected: false, kind: '', interface: '', address: '', gateway: '' });
          uplink.wifi = { ...uplink.wifi, state: 'searching', ssid: '', signalDbm: 0 };
          d.router.status.ethernetDiagnostics = null;
        });
      },
      renewDhcpLease: () => wait(1200),
      rescanEthernetPort: () => wait(1200),
      runSpeedtest: async () => {
        await wait(4000);
        change((d) => {
          d.router.status.speedtest.last = {
            server: 'San Jose, CA (SJC)',
            ageSeconds: 0,
            downloadMbps: 880 + Math.random() * 80,
            uploadMbps: 820 + Math.random() * 80,
            pingMs: 3 + Math.random() * 3,
          };
        });
      },

      setVpnEnabled: async (enabled) => {
        await wait(1000);
        change((d) => {
          d.router.vpnSettings.enabled = enabled;
          d.router.status.vpn = {
            ...d.router.status.vpn,
            enabled,
            state: enabled ? 'up' : 'off',
            handshakeAgeSeconds: enabled ? 1 : d.router.status.vpn.handshakeAgeSeconds,
          };
        });
      },
      setKillSwitch: async (killSwitch) => {
        await wait(700);
        change((d) => {
          d.router.vpnSettings.killSwitch = killSwitch;
          const vpn = d.router.status.vpn;
          // A broken tunnel lets traffic out directly once the kill switch is off, and blocks it once on.
          const state = vpn.state === 'down' && !killSwitch ? 'direct' : vpn.state === 'direct' ? 'down' : vpn.state;
          d.router.status.vpn = { ...vpn, killSwitch, state };
        });
      },
      setActiveVpnProfile: async (name) => {
        await wait(1200);
        change((d) => {
          const profile = d.router.vpnSettings.profiles.find((candidate) => candidate.name === name);
          d.router.vpnSettings.activeProfile = name;
          d.router.status.vpn = { ...d.router.status.vpn, endpoint: profile?.endpoint ?? '', handshakeAgeSeconds: 1 };
        });
      },
      removeVpnProfile: async (name) => {
        await wait(800);
        change((d) => {
          const settings = d.router.vpnSettings;
          settings.profiles = settings.profiles.filter((profile) => profile.name !== name);
          if (settings.activeProfile === name) settings.activeProfile = settings.profiles[0]?.name ?? '';
          if (settings.profiles.length === 0) {
            settings.enabled = false;
            d.router.status.vpn = { ...d.router.status.vpn, enabled: false, state: 'off', endpoint: '' };
          }
        });
      },
      importVpnProfile: async ({ text, fileName }) => {
        await wait(1100);
        const problem = checkWireGuardFile(text);
        if (problem) throw new Error(problem);
        change((d) => {
          const settings = d.router.vpnSettings;
          const taken = new Set(settings.profiles.map((profile) => profile.name));
          let name = fileName || 'wireguard.conf';
          for (let n = 2; taken.has(name); n += 1)
            name = `${(fileName || 'wireguard.conf').replace(/\.conf$/, '')}-${n}.conf`;
          settings.profiles.push(profileFromFile(name, text));
          if (!settings.activeProfile) settings.activeProfile = name;
        });
      },
      saveVpnDetails: async (profileName, form) => {
        await wait(1000);
        const errors: Record<string, string> = {};
        if (form.endpoint && !/^[^\s:]+:\d+$/.test(form.endpoint.trim())) {
          errors.endpoint = 'Must look like vpn.example.com:51820.';
        }
        if (form.mtu && !(Number(form.mtu) >= 1280 && Number(form.mtu) <= 1500)) {
          errors.mtu = 'Must be a number from 1280 to 1500, or empty.';
        }
        if (form.persistentKeepalive && !(Number(form.persistentKeepalive) >= 0)) {
          errors.persistentKeepalive = 'Must be a number of seconds, or empty.';
        }
        if (!form.allowedIps.includes('0.0.0.0/0')) errors.allowedIps = 'Must include 0.0.0.0/0.';
        failIfAny(errors);
        const list = (text: string) =>
          text
            .split(',')
            .map((item) => item.trim())
            .filter(Boolean);
        change((d) => {
          const profile = d.router.vpnSettings.profiles.find((candidate) => candidate.name === profileName);
          if (!profile) return;
          Object.assign(profile, {
            endpoint: form.endpoint.trim(),
            peerPublicKey: form.peerPublicKey.trim(),
            addresses: list(form.addresses),
            dns: list(form.dns),
            allowedIps: list(form.allowedIps),
            persistentKeepalive: Number(form.persistentKeepalive) || 0,
            mtu: Number(form.mtu) || 0,
            routedIpv6Prefix: form.routedIpv6Prefix.trim(),
            hasPresharedKey: profile.hasPresharedKey || form.presharedKey.trim() !== '',
          });
          if (form.privateKey.trim()) profile.publicKey = randomKey();
        });
      },
      generateVpnKey: async (profileName) => {
        await wait(900);
        change((d) => {
          const profile = d.router.vpnSettings.profiles.find((candidate) => candidate.name === profileName);
          if (profile) profile.publicKey = randomKey();
        });
      },

      saveHotspotSettings: async (settings) => {
        await wait(1500);
        failIfAny(checkHotspotSettings(settings));
        change((d) => {
          const { password, ...rest } = settings;
          d.router.hotspotSettings = rest;
          if (password) d.router.hotspotPassword = password;
          const { ssid, band, channel, security, metered } = rest;
          // Channel 0 means the band's default channel.
          const shown = channel || (band === '5GHz' ? 36 : 6);
          Object.assign(d.router.status.hotspot, { ssid, band, channel: shown, security, metered });
        });
      },
      saveGuestSettings: async (settings) => {
        await wait(1500);
        failIfAny(checkGuestSettings(settings));
        change((d) => {
          const { password, ...rest } = settings;
          d.router.guestSettings = { ...rest, hasPassword: rest.hasPassword || Boolean(password) };
          const guest = d.router.status.guest;
          d.router.status.guest = {
            enabled: rest.enabled,
            running: rest.enabled,
            ssid: rest.ssid,
            clients: rest.enabled ? guest.clients : [],
          };
        });
      },

      setClientInternetAccess: async (mac, allowed) => {
        await wait(700);
        change((d) => {
          const { status } = d.router;
          for (const client of [...status.clients, ...status.guest.clients]) {
            if (client.mac !== mac) continue;
            client.internetAccess = allowed;
            client.pausedSeconds = allowed ? undefined : 0;
            if (!allowed) Object.assign(client, { downMbps: 0, upMbps: 0 });
          }
        });
      },
      disconnectClient: () => wait(900),
      flushClientLease: () => wait(900),

      saveSshSettings: async (settings) => {
        await wait(900);
        if (settings.enabled && settings.authorizedKeys.trim() === '') {
          throw new Error('Add at least one public key before turning SSH on.');
        }
        change((d) => void (d.router.sshSettings = settings));
      },
      setHttpsEnabled: async (enabled) => {
        await wait(1000);
        change((d) => {
          const https = d.router.httpsSettings;
          https.enabled = enabled;
          if (enabled && !https.fingerprint) {
            Object.assign(https, {
              subject: 'CN=rasbeacon.local',
              fingerprint: randomFingerprint(),
              validUntil: '2036-10-06',
            });
          }
        });
      },
      makeNewCertificate: async () => {
        await wait(1500);
        change((d) => {
          Object.assign(d.router.httpsSettings, {
            uploaded: false,
            subject: 'CN=rasbeacon.local',
            fingerprint: randomFingerprint(),
            validUntil: '2036-10-06',
          });
        });
      },
      uploadCertificate: async ({ certificate, privateKey }) => {
        const [certificateText, keyText] = await Promise.all([certificate.text(), privateKey.text()]);
        await wait(1200);
        if (!certificateText.includes('BEGIN CERTIFICATE')) {
          throw new Error(`${certificate.name} is not a PEM certificate.`);
        }
        if (!/BEGIN (RSA |EC )?PRIVATE KEY/.test(keyText))
          throw new Error(`${privateKey.name} is not a PEM private key.`);
        change((d) => {
          Object.assign(d.router.httpsSettings, {
            uploaded: true,
            subject: 'CN=router.example.net',
            fingerprint: randomFingerprint(),
            validUntil: '2027-10-06',
          });
        });
      },

      installUpdate: async (file) => {
        await wait(4000);
        if (!file.name.endsWith('.rbupdate')) throw new Error('This is not a RasBeacon update file (.rbupdate).');
        change((d) => void (d.router.status.update.installing = true));
        // The router restarts into the new version a little later.
        setTimeout(
          () =>
            change((d) => {
              d.router.status.version = 'v1.1.0';
              Object.assign(d.router.status.update, {
                installing: false,
                message: 'Updated to v1.1.0.',
                latestVersion: undefined,
                latestUrl: undefined,
              });
            }),
          8000,
        );
      },
      setCheckOnline: async (enabled) => {
        await wait(700);
        change((d) => {
          Object.assign(
            d.router.status.update,
            enabled
              ? {
                  checkOnline: true,
                  latestVersion: 'v1.1.0',
                  latestUrl: 'https://github.com/rasbeacon/rasbeacon/releases/tag/v1.1.0',
                }
              : { checkOnline: false, latestVersion: undefined, latestUrl: undefined },
          );
        });
      },
      reboot: () => wait(3000),
      factoryReset: async (adminPassword) => {
        await wait(1500);
        if (adminPassword === '' || adminPassword === 'wrong') throw new Error('Wrong password.');
        change((d) => {
          d.phase = { kind: 'setup' };
          d.router.setup = { ...d.router.setup, mode: 'setup', claimed: false };
        });
      },
    };
    // In the "every change fails" state, each action that changes something fails instead.
    return failActions ? failingActions(all) : all;
  }, [failActions]);

  const state = useMemo<DeviceState>(() => {
    const { router, loading, errors } = demo;
    function loadable<T>(key: DataKey, data: T): Loadable<T> {
      return { data: loading[key] ? undefined : data, error: errors[key] ?? null };
    }
    return {
      phase: demo.phase,
      setup: router.setup,
      status: loadable('status', router.status),
      hotspotSettings: loadable('hotspotSettings', router.hotspotSettings),
      guestSettings: loadable('guestSettings', router.guestSettings),
      uplinkSettings: loadable('uplinkSettings', router.uplinkSettings),
      vpnSettings: loadable('vpnSettings', router.vpnSettings),
      sshSettings: loadable('sshSettings', router.sshSettings),
      httpsSettings: loadable('httpsSettings', router.httpsSettings),
      nearbyNetworks: demo.nearbyNetworks,
      portal: loadable('portal', router.status.portal),
      portalBrowser: demo.portalBrowser,
      diagnosticsUrl: `data:text/plain;charset=utf-8,${encodeURIComponent('RasBeacon diagnostics (demo)\n')}`,
    };
  }, [demo]);

  // The one action that answers with a value from the state, so it is made with the state.
  const { hotspotPassword } = demo.router;
  const revealHotspotPassword = useCallback(
    async (adminPassword: string) => {
      await wait(800);
      if (failActions) throw new Error(UNREACHABLE);
      if (adminPassword === '' || adminPassword === 'wrong') throw new Error('Wrong password.');
      return hotspotPassword;
    },
    [hotspotPassword, failActions],
  );

  const value = useMemo<Device>(
    () => ({ ...state, ...actions, revealHotspotPassword }),
    [state, actions, revealHotspotPassword],
  );

  return (
    <DeviceContext value={value}>
      {pathname === '/demo' ? <DemoIndex /> : children}
      <DemoTag />
      <StressToggle />
    </DeviceContext>
  );
}
