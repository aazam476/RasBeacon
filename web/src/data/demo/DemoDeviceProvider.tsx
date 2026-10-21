import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
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
import type { Plugin, PluginChoices, PluginPreview, SavedNetwork, VPNProfile } from '../types';
import { DemoIndex } from './DemoIndex';
import { DemoTag } from './DemoTag';
import { StressToggle } from './StressToggle';
import { LOGIN_PAGE_PICTURE } from './loginPagePicture';
import {
  catalogEntries,
  DEMO_DRIVES,
  DEMO_LOG,
  definitionOf,
  demoMetrics,
  installedPlugin,
  PLUGIN_DEFINITIONS,
  previewOf,
} from './plugins';
import { demoQrSvg } from './qrPicture';
import {
  checkGuestSettings,
  checkHotspotSettings,
  checkNewAdminPassword,
  checkNewNetwork,
  checkSetupForm,
  checkWifiPassword,
  checkWireGuardFile,
} from './rules';
import { CLOSED_BROWSER, createDemoState, SCENARIOS, UNREACHABLE, type DemoState } from './scenarios';

/**
 * Like the router: devices that used the data limit are paused until the counters are reset or the
 * limit is raised.
 */
function applyUsageLimit(d: DemoState) {
  const { limitMb } = d.router.usageSettings;
  const limit = limitMb > 0 ? limitMb * 1_000_000 : 0;
  let paused = 0;
  for (const client of d.router.status.clients) {
    const over = limit > 0 && client.downBytes + client.upBytes >= limit;
    if (over) paused += 1;
    if (over === Boolean(client.limitReached)) continue;
    client.limitReached = over || undefined;
    client.internetAccess = !over;
    if (over) Object.assign(client, { downMbps: 0, upMbps: 0 });
  }
  d.router.usageSettings.devicesPaused = paused;
}

/** The demo state as last shown, for actions that answer from it (plugin previews). */
let currentDemo: DemoState = createDemoState();

/** A new name for a plugin file waiting in the install sheet. */
let stagingCount = 0;
function newStagingId(kind: string): string {
  stagingCount += 1;
  return `${kind}-${stagingCount}`;
}

/** Now, as the router writes times. */
function nowIso(): string {
  return new Date().toISOString();
}

/** The plugin files waiting in the install sheet: staging ID → plugin ID and where it came from. */
const staged = new Map<string, { id: string; source: string }>();

/** Like the router: the choices for a plugin's ports and settings, checked. */
function checkPluginChoices(
  preview: Pick<PluginPreview, 'ports' | 'settings'>,
  choices: PluginChoices,
  taken: number[],
) {
  const errors: Record<string, string> = {};
  for (const port of preview.ports) {
    const host = choices.ports[port.key] ?? port.host;
    if (!(host >= 1 && host <= 65535)) errors[`ports.${port.key}`] = 'Choose a port from 1 to 65535.';
    else if ([22, 53, 67, 80, 443].includes(host))
      errors[`ports.${port.key}`] = `Port ${host} is RasBeacon's own. Choose another.`;
    else if (taken.includes(host)) errors[`ports.${port.key}`] = `Another plugin already uses port ${host}.`;
  }
  for (const setting of preview.settings) {
    const value = (choices.settings[setting.key] ?? '').trim();
    if (setting.required && value === '' && !(setting.type === 'password' && setting.hasValue)) {
      errors[`settings.${setting.key}`] = 'Fill this in.';
    }
    if (setting.type === 'number' && value !== '') {
      const n = Number(value);
      if (
        Number.isNaN(n) ||
        (setting.min !== undefined && n < setting.min) ||
        (setting.max !== undefined && n > setting.max)
      ) {
        errors[`settings.${setting.key}`] = `Must be from ${setting.min ?? 'any'} to ${setting.max ?? 'any'}.`;
      }
    }
  }
  if (Object.keys(errors).length > 0) throw new FieldErrors(errors);
}

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
  // The actions are made once; the plugin previews read the state as it is now.
  useEffect(() => {
    currentDemo = demo;
  });

  // Every action but one, made once: they change the state with `setDemo`.
  const actions = useMemo<Omit<DeviceActions, 'revealHotspotPassword' | 'wifiQr'>>(() => {
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

    const all: Omit<DeviceActions, 'revealHotspotPassword' | 'wifiQr'> = {
      retry: async () => {
        await wait(900);
        change((d) => {
          if (
            d.phase.kind === 'unreachable' ||
            d.phase.kind === 'loading' ||
            (d.phase.kind === 'notice' && d.phase.notice === 'off')
          )
            d.phase = { kind: 'ready' };
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
        change((d) => {
          d.router.status.speedtest.running = true;
        });
        await wait(4000);
        change((d) => {
          d.router.status.speedtest.running = false;
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
            // Off with the kill switch on: nothing gets through.
            state: enabled ? 'up' : d.router.vpnSettings.killSwitch ? 'blocked' : 'off',
            handshakeAgeSeconds: enabled ? 1 : d.router.status.vpn.handshakeAgeSeconds,
          };
        });
      },
      setKillSwitch: async (killSwitch) => {
        await wait(700);
        change((d) => {
          d.router.vpnSettings.killSwitch = killSwitch;
          const vpn = d.router.status.vpn;
          // A broken tunnel lets traffic out directly once the kill switch is off, and blocks it once on;
          // with the VPN off, the kill switch blocks everything.
          const state = !vpn.enabled
            ? killSwitch
              ? 'blocked'
              : 'off'
            : vpn.state === 'down' && !killSwitch
              ? 'direct'
              : vpn.state === 'direct'
                ? 'down'
                : vpn.state;
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
      renameVpnProfile: async (name, newName) => {
        await wait(600);
        if (newName !== name && currentDemo.router.vpnSettings.profiles.some((profile) => profile.name === newName)) {
          throw new FieldErrors({ name: 'Another profile already has this name.' });
        }
        change((d) => {
          const settings = d.router.vpnSettings;
          const profile = settings.profiles.find((p) => p.name === name);
          if (profile) profile.name = newName;
          if (settings.activeProfile === name) settings.activeProfile = newName;
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
          applyUsageLimit(d);
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
      renameClient: async (mac, name) => {
        await wait(600);
        const trimmed = name.trim();
        if ([...trimmed].length > 40) throw new FieldErrors({ name: 'Use at most 40 characters.' });
        change((d) => {
          const { status } = d.router;
          for (const client of [...status.clients, ...status.guest.clients]) {
            if (client.mac === mac) client.name = trimmed || undefined;
          }
        });
      },
      saveUsageLimit: async (limitMb) => {
        await wait(700);
        change((d) => {
          d.router.usageSettings.limitMb = limitMb;
          applyUsageLimit(d);
        });
      },
      resetUsage: async () => {
        await wait(700);
        change((d) => {
          const { status } = d.router;
          for (const client of [...status.clients, ...status.guest.clients]) {
            client.downBytes = 0;
            client.upBytes = 0;
          }
          d.router.usageSettings.since = nowIso();
          applyUsageLimit(d);
        });
      },

      saveSwapSettings: async (sizeGb) => {
        await wait(1500);
        change((d) => {
          d.router.swapSettings.sizeGb = sizeGb;
          d.router.status.system.swapTotalMb = sizeGb * 1024;
          d.router.status.system.swapUsedMb = Math.min(d.router.status.system.swapUsedMb, sizeGb * 1024);
        });
      },
      saveDnsSettings: async (text, withVpn) => {
        await wait(800);
        const servers = text
          .split(/[,\s]+/)
          .map((item) => item.trim())
          .filter(Boolean);
        // Like the router: addresses stay as typed; DoH addresses are written in full.
        const isIp = (server: string) => /^[\d.]+$|^[\da-f:]+$/i.test(server);
        const full = (server: string) => {
          const url = server.includes('://') ? server : `https://${server}`;
          if (!/^https:\/\/[a-z0-9.-]+\.[a-z]{2,}(:\d+)?(\/\S*)?$/i.test(url)) return null;
          return /^https:\/\/[^/]+\/?$/.test(url) ? `${url.replace(/\/$/, '')}/dns-query` : url;
        };
        const bad = servers.find((server) => !isIp(server) && !full(server));
        if (bad) {
          throw new FieldErrors({
            servers: `"${bad}" is not an IP address or a DNS-over-HTTPS address like https://dns.example/dns-query.`,
          });
        }
        if (servers.length > 4) throw new FieldErrors({ servers: 'At most 4 DNS servers.' });
        // In the order typed, as the router keeps them.
        const ordered = servers.map((server) => (isIp(server) ? server : full(server)!.toLowerCase()));
        change((d) => void (d.router.dnsSettings = { servers: ordered, withVpn }));
      },
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
              subject: 'CN=RasBeacon 7162',
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
            subject: 'CN=RasBeacon 7162',
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
      saveUpdateSettings: async (settings) => {
        await wait(600);
        if (settings.notifyUrl && !/^https?:\/\/[^/\s]+/.test(settings.notifyUrl)) {
          throw new FieldErrors({ notifyUrl: 'Enter a web address like https://ntfy.sh/your-topic.' });
        }
        change((d) => {
          d.router.updateSettings = settings;
          d.router.status.update.checkHours = settings.checkHours;
        });
      },
      saveAccessSettings: async (settings) => {
        await wait(1200);
        const allowed: string[] = [];
        for (const text of settings.allowed) {
          const mac = text.trim().toLowerCase().replaceAll('-', ':');
          if (!/^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/.test(mac)) {
            throw new FieldErrors({ allowed: `"${text.trim()}" is not a MAC address such as 3a:1f:22:aa:bb:cc.` });
          }
          if (!allowed.includes(mac)) allowed.push(mac);
        }
        if (settings.allowListOn && allowed.length === 0) {
          throw new FieldErrors({ allowed: 'Add at least one device, or nobody could join over Wi-Fi.' });
        }
        if (settings.maxDevices < 0 || settings.maxDevices > 231) {
          throw new FieldErrors({ maxDevices: 'Choose a number from 1 to 231, or no limit.' });
        }
        change((d) => void (d.router.accessSettings = { ...settings, allowed }));
      },
      checkForUpdates: async () => {
        await wait(1500);
        change((d) => {
          Object.assign(d.router.status.update, {
            lastChecked: new Date().toISOString(),
            checkError: undefined,
            latestVersion: 'v1.1.0',
            latestUrl: 'https://github.com/rasbeacon/rasbeacon/releases/tag/v1.1.0',
          });
        });
      },
      testUpdateNotification: async (url) => {
        await wait(1000);
        if (!url && !currentDemo.router.updateSettings.notifyUrl) {
          throw new FieldErrors({ notifyUrl: 'Enter the address first.' });
        }
      },
      reboot: () => wait(3000),
      shutDown: async () => {
        await wait(1500);
        change((d) => void (d.phase = { kind: 'notice', notice: 'off' }));
      },
      saveHostname: async (hostname) => {
        await wait(1200);
        const name = hostname.trim().toLowerCase();
        if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(name)) {
          throw new FieldErrors({
            hostname: 'Use 1 to 63 lowercase letters, digits and hyphens, not starting or ending with a hyphen.',
          });
        }
        if (currentDemo.router.status.clients.some((c) => c.hostname.toLowerCase() === name)) {
          throw new FieldErrors({ hostname: 'A device on the hotspot already uses this name.' });
        }
        await wait(3000);
        change((d) => {
          d.router.status.device.hostname = `${name}.lan`;
        });
      },
      previewPluginFile: async (file) => {
        await wait(1200);
        if (!file.name.endsWith('.rba')) throw new FieldErrors({ file: 'This is not a RasBeacon plugin (.rba) file.' });
        // The file's name says which of the sample plugins it is; anything else is Trip Notes.
        const definition =
          PLUGIN_DEFINITIONS.find((d) => file.name.startsWith(d.id.split('-')[0])) ?? definitionOf('trip-notes')!;
        const stagingId = newStagingId('file');
        staged.set(stagingId, { id: definition.id, source: 'file' });
        return { ...previewOf(definition, currentDemo.router.plugins, stagingId), source: 'file' };
      },
      pluginCatalog: async (refresh) => {
        await wait(refresh ? 1600 : 500);
        if (refresh) change((d) => void (d.router.plugins.checkedAt = nowIso()));
        const plugins = currentDemo.router.plugins;
        return { entries: catalogEntries(plugins), checkedAt: refresh ? nowIso() : plugins.checkedAt };
      },
      previewCatalogPlugin: async (source, file) => {
        await wait(1800);
        const definition = PLUGIN_DEFINITIONS.find((d) => d.source === source && d.file === file);
        if (!definition)
          throw new FieldErrors({ file: "This plugin isn't in the catalog any more. Check the catalog again." });
        const stagingId = newStagingId('catalog');
        staged.set(stagingId, { id: definition.id, source });
        return previewOf(definition, currentDemo.router.plugins, stagingId);
      },
      previewPluginUpdate: async (id) => {
        await wait(1800);
        const definition = definitionOf(id);
        if (!definition) throw new Error('No update for this plugin is in the catalog.');
        const stagingId = newStagingId('update');
        staged.set(stagingId, { id, source: definition.source });
        return previewOf(definition, currentDemo.router.plugins, stagingId);
      },
      installPlugin: async (stagingId, choices) => {
        await wait(700);
        const entry = staged.get(stagingId);
        const definition = entry && definitionOf(entry.id);
        if (!entry || !definition) throw new Error('This plugin file is gone. Choose it again.');
        const plugins = currentDemo.router.plugins;
        const preview = previewOf(definition, plugins, stagingId);
        const taken = plugins.plugins
          .filter((p) => p.id !== definition.id)
          .flatMap((p) => p.ports.map((port) => port.host));
        checkPluginChoices(preview, choices, taken);
        staged.delete(stagingId);
        const existing = plugins.plugins.find((p) => p.id === definition.id);
        const address = existing?.address ?? `10.89.0.${2 + plugins.plugins.length}`;
        change((d) => {
          const list = d.router.plugins.plugins;
          const plugin: Plugin = {
            ...installedPlugin(definition, address, choices, 'installing'),
            source: entry.source,
            message: definition.downloadSize > 0 ? 'Downloading its image…' : 'Unpacking its image…',
          };
          const index = list.findIndex((p) => p.id === definition.id);
          if (index >= 0) list[index] = plugin;
          else list.push(plugin);
        });
        // A moment later it runs.
        setTimeout(
          () =>
            change((d) => {
              const plugin = d.router.plugins.plugins.find((p) => p.id === definition.id);
              if (!plugin || plugin.state !== 'installing') return;
              Object.assign(plugin, {
                state: 'running',
                message: undefined,
                healthy: true,
                metrics: demoMetrics(plugin.id),
              });
            }),
          3000,
        );
      },
      cancelPluginInstall: async (stagingId) => {
        staged.delete(stagingId);
      },
      savePluginSettings: async (id, choices) => {
        await wait(1000);
        const plugins = currentDemo.router.plugins;
        const plugin = plugins.plugins.find((p) => p.id === id);
        if (!plugin) throw new Error('No such plugin is installed.');
        const taken = plugins.plugins.filter((p) => p.id !== id).flatMap((p) => p.ports.map((port) => port.host));
        checkPluginChoices(plugin, choices, taken);
        change((d) => {
          const target = d.router.plugins.plugins.find((p) => p.id === id);
          if (!target) return;
          target.ports = target.ports.map((port) => {
            const host = choices.ports[port.key] ?? port.host;
            return { ...port, host, url: port.url ? `http://192.168.8.1:${host}/` : undefined };
          });
          target.settings = target.settings.map((setting) =>
            setting.type === 'password'
              ? { ...setting, hasValue: setting.hasValue || Boolean(choices.settings[setting.key]) }
              : { ...setting, value: choices.settings[setting.key] ?? setting.value },
          );
          target.drive = choices.drive || undefined;
        });
      },
      setPluginRunning: async (id, running) => {
        await wait(1200);
        change((d) => {
          const plugin = d.router.plugins.plugins.find((p) => p.id === id);
          if (!plugin) return;
          Object.assign(
            plugin,
            running
              ? { state: 'running', message: undefined, healthy: true, metrics: demoMetrics(id) }
              : {
                  state: 'stopped',
                  message: undefined,
                  healthy: undefined,
                  metrics: plugin.metrics.map((m) => ({ ...m, value: '' })),
                },
          );
        });
      },
      removePlugin: async (id) => {
        await wait(1200);
        change((d) => void (d.router.plugins.plugins = d.router.plugins.plugins.filter((p) => p.id !== id)));
      },
      pluginLogs: async (id) => {
        await wait(400);
        return id === 'adguard-home' ? DEMO_LOG : [];
      },
      pluginDrives: async () => {
        await wait(500);
        const plugins = currentDemo.router.plugins.plugins;
        return DEMO_DRIVES.map((drive) => ({ ...drive, usedBy: plugins.find((p) => p.drive === drive.uuid)?.name }));
      },
      savePluginSources: async (sources) => {
        await wait(700);
        const errors: Record<string, string> = {};
        sources.forEach((source, index) => {
          if (!/^[A-Za-z0-9-]+\/[A-Za-z0-9._-]+$/.test(source.trim())) {
            errors[`sources.${index}`] = 'Enter a GitHub repository like owner/repo.';
          }
        });
        failIfAny(errors);
        change((d) => {
          // The built-in source only if it is still listed (it can be removed, and added back).
          const builtIn = 'aazam476/RasBeacon-plugins';
          const unique = [...new Set(sources.map((source) => source.trim()))];
          d.router.plugins.sources = unique.map((source) => ({
            source,
            builtIn: source.toLowerCase() === builtIn.toLowerCase(),
          }));
        });
      },

      backUp: async (backupPassword) => {
        await wait(1500);
        if (backupPassword.length < 8) throw new FieldErrors({ backupPassword: 'Use at least 8 characters.' });
        // The demo's "backup" is a small text file.
        const url = URL.createObjectURL(new Blob(['RasBeacon demo backup\n'], { type: 'application/octet-stream' }));
        const link = document.createElement('a');
        link.href = url;
        link.download = 'rasbeacon-backup-demo.rbbackup';
        link.click();
        setTimeout(() => URL.revokeObjectURL(url), 60_000);
      },
      restore: async (file, backupPassword) => {
        await wait(2000);
        const errors: Record<string, string> = {};
        if (!file.name.endsWith('.rbbackup')) errors.file = 'This is not a RasBeacon backup (.rbbackup) file.';
        if (backupPassword === 'wrong' || backupPassword.length < 8) {
          errors.backupPassword = 'The password is wrong, or the backup file is damaged.';
        }
        failIfAny(errors);
      },

      changeAdminPassword: async (form) => {
        await wait(1200);
        const errors = checkNewAdminPassword(form);
        if (form.currentPassword === 'wrong') errors.currentPassword = 'Wrong password.';
        failIfAny(errors);
      },
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
      status: loadable('status', {
        ...router.status,
        plugins: router.plugins.plugins.map(({ id, name, state, icon }) => ({ id, name, state, icon })),
      }),
      hotspotSettings: loadable('hotspotSettings', router.hotspotSettings),
      guestSettings: loadable('guestSettings', router.guestSettings),
      uplinkSettings: loadable('uplinkSettings', router.uplinkSettings),
      vpnSettings: loadable('vpnSettings', router.vpnSettings),
      sshSettings: loadable('sshSettings', router.sshSettings),
      dnsSettings: loadable('dnsSettings', router.dnsSettings),
      swapSettings: loadable('swapSettings', router.swapSettings),
      updateSettings: loadable('updateSettings', router.updateSettings),
      accessSettings: loadable('accessSettings', router.accessSettings),
      plugins: loadable('plugins', router.plugins),
      usageSettings: loadable('usageSettings', router.usageSettings),
      httpsSettings: loadable('httpsSettings', router.httpsSettings),
      nearbyNetworks: demo.nearbyNetworks,
      portal: loadable('portal', router.status.portal),
      portalBrowser: demo.portalBrowser,
      diagnosticsUrl: `data:text/plain;charset=utf-8,${encodeURIComponent('RasBeacon diagnostics (demo)\n')}`,
    };
  }, [demo]);

  // The one action that answers with a value from the state, so it is made with the state.
  const { hotspotPassword } = demo.router;
  const revealHotspotPassword = useCallback(async () => {
    await wait(500);
    if (failActions) throw new Error(UNREACHABLE);
    return hotspotPassword;
  }, [hotspotPassword, failActions]);
  const { ssid: hotspotSsid } = demo.router.hotspotSettings;
  const { enabled: guestEnabled, ssid: guestSsid } = demo.router.guestSettings;
  const wifiQr = useCallback(
    async (network: 'hotspot' | 'guest') => {
      await wait(600);
      if (failActions) throw new Error(UNREACHABLE);
      if (network === 'hotspot') {
        return { ssid: hotspotSsid, svg: demoQrSvg(`${hotspotSsid}:${hotspotPassword}`) };
      }
      if (!guestEnabled) throw new Error('The guest network is off.');
      return { ssid: guestSsid, svg: demoQrSvg(guestSsid) };
    },
    [failActions, hotspotSsid, hotspotPassword, guestEnabled, guestSsid],
  );

  const value = useMemo<Device>(
    () => ({ ...state, ...actions, revealHotspotPassword, wifiQr }),
    [state, actions, revealHotspotPassword, wifiQr],
  );

  return (
    <DeviceContext value={value}>
      {pathname === '/demo' ? <DemoIndex /> : children}
      <DemoTag />
      <StressToggle />
    </DeviceContext>
  );
}
