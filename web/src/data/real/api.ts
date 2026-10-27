/**
 * The router's API (`/api/v1`, served by rbweb; docs/api.md lists every route). One function per
 * route, so URLs, JSON, the CSRF header, time limits and errors live in one place. Only the real
 * provider (RealDeviceProvider.tsx) calls these.
 */

import type {
  AccessSettings,
  ClaimResponse,
  DNSSettings,
  ModemSettings,
  WelcomeSettings,
  PortForwardingSettings,
  PortForwardRule,
  GuestSettings,
  HotspotChannelsResponse,
  HotspotSettings,
  HTTPSSettings,
  NearbyNetwork,
  Plugin,
  PluginCatalog,
  PluginChoices,
  PluginDrive,
  PluginLogsResponse,
  PluginPreview,
  PluginsState,
  PortalStatus,
  SetupState,
  Speedtest,
  SSHSettings,
  Status,
  SwapSettings,
  UpdateSettings,
  UpdateStatus,
  UplinkSettings,
  UsageSettings,
  QoSSettings,
  RadioRole,
  VPNProfile,
  VPNSettings,
  WiFiQR,
} from '../types';

/** The API's answer when something went wrong. `field` names the rejected input, if any. */
interface ErrorResponse {
  error: string;
  field?: string;
}

export interface ClaimRequest {
  adminPassword: string;
  hotspotSsid: string;
  hotspotPassword: string;
  hotspotBand: string;
}

export interface SessionInfo {
  loggedIn: boolean;
  csrfToken?: string;
}

/** GET /portal/browser/frame: a picture of the login page in RasBeacon's browser. */
export interface FrameResponse {
  running: boolean;
  seq: number;
  /** Base64 JPEG. */
  image: string;
  width: number;
  height: number;
  url: string;
  title: string;
}

/** POST /portal/browser/input: something done on the picture. */
export type BrowserInput =
  | { action: 'click'; x: number; y: number }
  | { action: 'scroll'; x: number; y: number; deltaY: number }
  | { action: 'type'; text: string }
  | { action: 'key'; key: string }
  | { action: 'back' }
  | { action: 'reload' };

/** POST /settings/vpn/profile/save: one profile's details; empty keys keep the saved ones. */
export interface VPNProfileDetails extends VPNProfile {
  privateKey?: string;
  presharedKey?: string;
  removePresharedKey?: boolean;
}

/** Thrown when the API answers with an error, or doesn't answer. */
export class ApiError extends Error {
  /** The HTTP status; 0 when there was no answer. */
  readonly status: number;
  readonly field?: string;

  constructor(status: number, message: string, field?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.field = field;
  }
}

export const UNREACHABLE = "Can't reach RasBeacon. Check that this device is connected to its Wi-Fi.";

// The CSRF token of this browser's session. Every request that changes something sends it
// (see internal/webserver/sessions.go for why).
let csrfToken = '';

/** How long a request may take unless the route says otherwise. */
const DEFAULT_TIMEOUT_MS = 20_000;

interface RequestOptions {
  body?: unknown;
  /** Sent as the raw body (update files). */
  file?: Blob;
  /** How long to wait for the answer. Ignored for uploads (`file`), which have no limit. */
  timeoutMs?: number;
  /** Set on the second try after a refused CSRF token, so it is retried only once. */
  retried?: boolean;
}

/** rbweb's `field` on a 403 that refused the CSRF token (internal/webserver/server.go). */
const CSRF_FIELD = 'csrfToken';

/** The token fetch in progress, shared by every request that was refused at the same time. */
let tokenRefresh: Promise<unknown> | null = null;

/**
 * Fetches a new CSRF token. After rbweb restarts (an update, unplugging the Pi), "Stay logged in"
 * starts a new session with a new token, and the open page still has the old one. The page then
 * sends many requests at once; they all wait for one fetch of /session instead of each making
 * their own.
 */
function refreshToken(): Promise<unknown> {
  tokenRefresh ??= getSession()
    .catch(() => undefined)
    .finally(() => {
      tokenRefresh = null;
    });
  return tokenRefresh;
}

/** Sends one request and returns the parsed answer (undefined for 204). */
async function request<T>(method: string, path: string, options: RequestOptions = {}): Promise<T> {
  const { body, file, timeoutMs = DEFAULT_TIMEOUT_MS } = options;
  const headers: Record<string, string> = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (file !== undefined) headers['Content-Type'] = 'application/octet-stream';
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken;

  const controller = new AbortController();
  // Uploads (updates, plugins, backups) get no time limit here: a large backup on a slow link can
  // take hours. rbweb ends an upload that stops sending data for 2 minutes
  // (internal/webserver/transfers.go), so a stuck one still fails.
  const timer = file === undefined ? setTimeout(() => controller.abort(), timeoutMs) : undefined;
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: file ?? (body === undefined ? undefined : JSON.stringify(body)),
      credentials: 'same-origin',
      cache: 'no-store',
      signal: controller.signal,
    });
  } catch {
    // No answer at all: the phone left the hotspot, or RasBeacon is restarting.
    throw new ApiError(0, UNREACHABLE);
  } finally {
    clearTimeout(timer);
  }

  if (!response.ok) {
    let message = `RasBeacon answered with an error (${response.status}).`;
    let field: string | undefined;
    try {
      const error = (await response.json()) as ErrorResponse;
      message = error.error || message;
      field = error.field;
    } catch {
      // Not JSON: keep the general message.
    }
    // Only a refused CSRF token is tried again, once, with a new token. Other 403s (such as a
    // wrong current password) are real answers, and sending them again would count as another
    // wrong try.
    if (response.status === 403 && field === CSRF_FIELD && !options.retried) {
      await refreshToken();
      return request<T>(method, path, { ...options, retried: true });
    }
    throw new ApiError(response.status, message, field);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

function rememberSession(session: SessionInfo): SessionInfo {
  csrfToken = session.csrfToken ?? '';
  return session;
}

// --- No login needed -----------------------------------------------------------------------------

export const getSetupState = () => request<SetupState>('GET', '/setup');

export async function claim(req: ClaimRequest): Promise<ClaimResponse> {
  const answer = await request<ClaimResponse>('POST', '/setup/claim', { body: req });
  // Claiming also logs this browser in; fetch the CSRF token for later requests.
  await getSession().catch(() => undefined);
  return answer;
}

export async function getSession(): Promise<SessionInfo> {
  return rememberSession(await request<SessionInfo>('GET', '/session'));
}

export async function logIn(password: string, stayLoggedIn: boolean): Promise<SessionInfo> {
  return rememberSession(await request<SessionInfo>('POST', '/session', { body: { password, stayLoggedIn } }));
}

export async function logOut(): Promise<void> {
  await request<void>('DELETE', '/session');
  csrfToken = '';
}

export const getPortalStatus = () => request<PortalStatus>('GET', '/portal');

/** Login mode: start, add 10 minutes, stop; "check" looks for a login page again right away. */
export const portalAction = (action: 'start' | 'extend' | 'stop' | 'check') =>
  // An empty JSON body: rbweb only accepts JSON for these public actions.
  request<PortalStatus>('POST', `/portal/${action}`, { body: {}, timeoutMs: 30_000 });

export const startPortalBrowser = (size: { width: number; height: number }) =>
  request<PortalStatus>('POST', '/portal/browser/start', { body: size, timeoutMs: 45_000 });

export const stopPortalBrowser = () => request<PortalStatus>('POST', '/portal/browser/stop', { body: {} });

/** Waits (about 10 seconds at most) for a picture newer than `after`. */
export const getPortalBrowserFrame = (after: number) =>
  request<FrameResponse>('GET', `/portal/browser/frame?after=${after}`, { timeoutMs: 25_000 });

export const sendPortalBrowserInput = (input: BrowserInput) =>
  request<void>('POST', '/portal/browser/input', { body: input });

// --- Login required ------------------------------------------------------------------------------

export const getStatus = () => request<Status>('GET', '/status');

export const getHotspotSettings = () => request<HotspotSettings>('GET', '/settings/hotspot');
export const putHotspotSettings = (settings: HotspotSettings) =>
  request<HotspotSettings>('PUT', '/settings/hotspot', { body: settings, timeoutMs: 45_000 });
export const revealHotspotPassword = () =>
  request<{ hotspotPassword: string }>('POST', '/settings/hotspot/reveal-password', { body: {} });
/** A QR code to join the hotspot or the guest network. */
export const getHotspotChannels = (country: string) =>
  request<HotspotChannelsResponse>('POST', '/settings/hotspot/channels', { body: { country } });
export const wifiQr = (network: 'hotspot' | 'guest') => request<WiFiQR>('POST', '/wifi-qr', { body: { network } });

export const getGuestSettings = () => request<GuestSettings>('GET', '/settings/guest');
export const putGuestSettings = (settings: GuestSettings) =>
  request<GuestSettings>('PUT', '/settings/guest', { body: settings, timeoutMs: 45_000 });

export const getSSHSettings = () => request<SSHSettings>('GET', '/settings/ssh');
export const putSSHSettings = (settings: SSHSettings) =>
  request<SSHSettings>('PUT', '/settings/ssh', { body: settings });

export const getUplinkSettings = () => request<UplinkSettings>('GET', '/settings/uplink');
export const putUplinkSettings = (settings: UplinkSettings) =>
  request<UplinkSettings>('PUT', '/settings/uplink', { body: settings, timeoutMs: 45_000 });
/** Looks for nearby Wi-Fi networks (about 5 seconds). */
export const scanNearbyNetworks = () =>
  request<{ networks: NearbyNetwork[] }>('POST', '/uplink/scan', { timeoutMs: 30_000 });
export const uplinkAction = (action: 'disconnect' | 'renew' | 'ethernet/scan') =>
  request<void>('POST', `/uplink/${action}`, { timeoutMs: 30_000 });
/** Cloudflare's speed test: about 20 seconds; the router stops it after 3 minutes. */
export const runSpeedtest = () => request<Speedtest>('POST', '/speedtest', { timeoutMs: 200_000 });

export const getSwapSettings = () => request<SwapSettings>('GET', '/settings/swap');
/** Making a large swap file can take a little while on a slow card. */
export const putSwapSettings = (sizeGb: number) =>
  request<SwapSettings>('PUT', '/settings/swap', { body: { sizeGb, maxGb: 0 }, timeoutMs: 120_000 });

export const getDNSSettings = () => request<DNSSettings>('GET', '/settings/dns');
export const putDNSSettings = (settings: DNSSettings) =>
  request<DNSSettings>('PUT', '/settings/dns', { body: settings, timeoutMs: 30_000 });

export const getPortForwarding = () => request<PortForwardingSettings>('GET', '/settings/port-forwarding');
/** Reloading the firewall takes a moment. */
export const putPortForwarding = (rules: PortForwardRule[]) =>
  request<PortForwardingSettings>('PUT', '/settings/port-forwarding', { body: { rules }, timeoutMs: 30_000 });

export const getModemSettings = () => request<ModemSettings>('GET', '/settings/modem');
export const putModemSettings = (settings: ModemSettings) =>
  request<ModemSettings>('PUT', '/settings/modem', { body: settings });

export const getWelcomeSettings = () => request<WelcomeSettings>('GET', '/settings/welcome');
/** Reloading the firewall takes a moment. */
export const putWelcomeSettings = (settings: WelcomeSettings) =>
  request<WelcomeSettings>('PUT', '/settings/welcome', { body: settings, timeoutMs: 30_000 });
export const resetWelcome = (mac: string) => request<void>('POST', '/clients/welcome-reset', { body: { mac } });

export const getVPNSettings = () => request<VPNSettings>('GET', '/settings/vpn');
export const putVPNSwitches = (switches: { enabled: boolean; killSwitch: boolean }) =>
  request<VPNSettings>('PUT', '/settings/vpn', { body: switches, timeoutMs: 30_000 });
export const importVPNProfile = (conf: string, name?: string) =>
  request<VPNSettings>('POST', '/settings/vpn/import', { body: { conf, name }, timeoutMs: 30_000 });
export const saveVPNProfile = (details: VPNProfileDetails) =>
  request<VPNSettings>('POST', '/settings/vpn/profile/save', { body: details, timeoutMs: 30_000 });
export const vpnProfileAction = (action: 'activate' | 'remove' | 'generate-key', name: string) =>
  request<VPNSettings>('POST', `/settings/vpn/profile/${action}`, { body: { name }, timeoutMs: 30_000 });

export const renameVPNProfile = (name: string, newName: string) =>
  request<VPNSettings>('POST', '/settings/vpn/profile/rename', { body: { name, newName } });

export const clientAction = (action: 'disconnect' | 'flush-lease', mac: string) =>
  request<void>('POST', `/clients/${action}`, { body: { mac } });
export const setClientInternetAccess = (mac: string, allowed: boolean) =>
  request<void>('POST', '/clients/internet-access', { body: { mac, allowed } });
export const renameClient = (mac: string, name: string) =>
  request<void>('POST', '/clients/name', { body: { mac, name } });
export const setFixedAddress = (mac: string, address: string) =>
  request<void>('POST', '/clients/fixed-address', { body: { mac, address } });

/** Radio MAC address to its role; radios left out are automatic. */
export const putRadioRoles = (roles: Record<string, RadioRole>) =>
  request<void>('PUT', '/settings/radios', { body: { roles } });
export const keepRadioRoles = () => request<void>('POST', '/settings/radios/keep');
export const revertRadioRoles = () => request<void>('POST', '/settings/radios/revert');

export const setClientPriority = (mac: string, priority: boolean) =>
  request<void>('POST', '/clients/priority', { body: { mac, priority } });
export const getQoSSettings = () => request<QoSSettings>('GET', '/settings/qos');
export const putQoSSettings = (speeds: { uploadKbps: number; downloadKbps: number }) =>
  request<QoSSettings>('PUT', '/settings/qos', { body: speeds });

export const getUsageSettings = () => request<UsageSettings>('GET', '/settings/usage');
export const putUsageSettings = (limitMb: number) =>
  request<UsageSettings>('PUT', '/settings/usage', { body: { limitMb } });
export const resetUsage = () => request<UsageSettings>('POST', '/clients/usage/reset');

export const getHTTPSSettings = () => request<HTTPSSettings>('GET', '/settings/https');
/** `generateNew`, `certificate` and `privateKey` are write-only actions. */
export const putHTTPSSettings = (
  settings: Pick<HTTPSSettings, 'enabled'> & { generateNew?: boolean; certificate?: string; privateKey?: string },
) => request<HTTPSSettings>('PUT', '/settings/https', { body: settings, timeoutMs: 45_000 });

/** Uploads and installs an update file; takes minutes on slow Wi-Fi. */
export const installUpdate = (file: File) => request<UpdateStatus>('POST', '/system/update', { file });
export const getAccessSettings = () => request<AccessSettings>('GET', '/settings/access');
export const putAccessSettings = (settings: AccessSettings) =>
  request<AccessSettings>('PUT', '/settings/access', { body: settings });
export const getUpdateSettings = () => request<UpdateSettings>('GET', '/settings/updates');
export const putUpdateSettings = (settings: UpdateSettings) =>
  request<UpdateStatus>('PUT', '/settings/updates', { body: settings });
export const checkForUpdates = () => request<UpdateStatus>('POST', '/system/update/check', { timeoutMs: 60_000 });
export const testUpdateNotification = (url: string) =>
  request<void>('POST', '/settings/updates/test', { body: { url }, timeoutMs: 30_000 });
export const reboot = () => request<void>('POST', '/system/reboot');
export const restartIntoUpdate = () => request<void>('POST', '/system/update/restart');
export const shutDown = () => request<void>('POST', '/system/shutdown');
export const saveHostname = (hostname: string) => request<void>('POST', '/system/hostname', { body: { hostname } });
export const changeAdminPassword = (currentPassword: string, newPassword: string) =>
  request<void>('POST', '/system/admin-password', { body: { currentPassword, newPassword } });
export const factoryReset = (adminPassword: string) =>
  request<void>('POST', '/system/factory-reset', { body: { password: adminPassword } });

/** The diagnostics report is a plain download. */
export const DIAGNOSTICS_URL = '/api/v1/diagnostics';

// --- Plugins (slow ones: GitHub and image registries through the VPN) ------------------------

export const getPlugins = () => request<PluginsState>('GET', '/plugins');
/** Uploads a .rba; it is checked before the install sheet opens. */
export const uploadPlugin = (file: File) => request<PluginPreview>('POST', '/plugins/upload', { file });
export const getPluginCatalog = (refresh: boolean) =>
  request<PluginCatalog>('POST', '/plugins/catalog', { body: { refresh }, timeoutMs: 120_000 });
export const downloadCatalogPlugin = (source: string, file: string) =>
  request<PluginPreview>('POST', '/plugins/catalog/download', { body: { source, file }, timeoutMs: 30 * 60_000 });
export const downloadPluginUpdate = (id: string) =>
  request<PluginPreview>('POST', '/plugins/update', { body: { id }, timeoutMs: 30 * 60_000 });
export const installPlugin = (stagingId: string, choices: PluginChoices, useKeptData = false) =>
  request<Plugin>('POST', '/plugins/install', { body: { stagingId, ...choices, useKeptData } });
export const cancelPluginInstall = (stagingId: string) =>
  request<void>('POST', '/plugins/cancel', { body: { stagingId } });
export const putPluginSettings = (id: string, choices: PluginChoices) =>
  request<Plugin>('PUT', '/plugins/settings', { body: { id, ...choices }, timeoutMs: 120_000 });
export const pluginAction = (action: 'start' | 'stop', id: string) =>
  request<PluginsState>('POST', `/plugins/${action}`, { body: { id }, timeoutMs: 120_000 });
export const removePlugin = (id: string, keepData: boolean) =>
  request<PluginsState>('POST', '/plugins/remove', { body: { id, keepData }, timeoutMs: 120_000 });
export const getPluginLogs = (id: string) => request<PluginLogsResponse>('POST', '/plugins/logs', { body: { id } });
export const getPluginDrives = () => request<{ drives: PluginDrive[] }>('GET', '/plugins/drives');
export const putPluginSources = (sources: string[]) =>
  request<PluginsState>('PUT', '/plugins/sources', { body: { sources } });

// --- Backups ---------------------------------------------------------------------------------

/**
 * Asks for a backup. The answer is a one-time link to it: the browser saves the file as it
 * arrives (with its own progress), instead of holding a large backup in memory first.
 */
export const prepareBackup = (password: string, plugins: string) =>
  request<{ token: string; fileName: string }>('POST', '/system/backup', { body: { password, plugins } });
/** Where the browser downloads a backup asked for with `prepareBackup`. */
export const backupDownloadUrl = (token: string) => `/api/v1/system/backup/${encodeURIComponent(token)}`;
export const uploadBackup = (file: File) => request<void>('POST', '/system/restore/upload', { file });
export const restoreBackup = (password: string) =>
  request<{ version: string; created: string; plugins: boolean }>('POST', '/system/restore', {
    body: { password },
    timeoutMs: 30 * 60_000,
  });
