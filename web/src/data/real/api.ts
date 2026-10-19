/**
 * The router's API (`/api/v1`, served by rbweb; docs/api.md lists every route). One function per
 * route, so URLs, JSON, the CSRF header, time limits and errors live in one place. Only the real
 * provider (RealDeviceProvider.tsx) calls these.
 */

import type {
  ClaimResponse,
  DNSSettings,
  GuestSettings,
  HotspotSettings,
  HTTPSSettings,
  NearbyNetwork,
  PortalStatus,
  SetupState,
  Speedtest,
  SSHSettings,
  SwapSettings,
  Status,
  UpdateStatus,
  UplinkSettings,
  UsageSettings,
  PluginCatalog,
  PluginChoices,
  PluginDrive,
  PluginLogsResponse,
  Plugin,
  PluginPreview,
  PluginsState,
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
  timeoutMs?: number;
}

/** Sends one request and returns the parsed answer (undefined for 204). */
async function request<T>(method: string, path: string, options: RequestOptions = {}): Promise<T> {
  const { body, file, timeoutMs = DEFAULT_TIMEOUT_MS } = options;
  const headers: Record<string, string> = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (file !== undefined) headers['Content-Type'] = 'application/octet-stream';
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken;

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
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
export const installUpdate = (file: File) =>
  request<UpdateStatus>('POST', '/system/update', { file, timeoutMs: 30 * 60_000 });
export const setCheckOnline = (checkOnline: boolean) =>
  request<UpdateStatus>('PUT', '/settings/updates', { body: { checkOnline } });
export const reboot = () => request<void>('POST', '/system/reboot');
export const factoryReset = (adminPassword: string) =>
  request<void>('POST', '/system/factory-reset', { body: { password: adminPassword } });

/** The diagnostics report is a plain download. */
export const DIAGNOSTICS_URL = '/api/v1/diagnostics';

// --- Plugins (slow ones: GitHub and image registries through the VPN) ------------------------

export const getPlugins = () => request<PluginsState>('GET', '/plugins');
/** Uploads a .rba; it is checked before the install sheet opens. */
export const uploadPlugin = (file: File) =>
  request<PluginPreview>('POST', '/plugins/upload', { file, timeoutMs: 30 * 60_000 });
export const getPluginCatalog = (refresh: boolean) =>
  request<PluginCatalog>('POST', '/plugins/catalog', { body: { refresh }, timeoutMs: 120_000 });
export const downloadCatalogPlugin = (source: string, file: string) =>
  request<PluginPreview>('POST', '/plugins/catalog/download', { body: { source, file }, timeoutMs: 30 * 60_000 });
export const downloadPluginUpdate = (id: string) =>
  request<PluginPreview>('POST', '/plugins/update', { body: { id }, timeoutMs: 30 * 60_000 });
export const installPlugin = (stagingId: string, choices: PluginChoices) =>
  request<Plugin>('POST', '/plugins/install', { body: { stagingId, ...choices } });
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
export const prepareBackup = (adminPassword: string, password: string, includePlugins: boolean) =>
  request<{ token: string; fileName: string }>('POST', '/system/backup', {
    body: { adminPassword, password, includePlugins },
  });
/** Where the browser downloads a backup asked for with `prepareBackup`. */
export const backupDownloadUrl = (token: string) => `/api/v1/system/backup/${encodeURIComponent(token)}`;
export const uploadBackup = (file: File) =>
  request<void>('POST', '/system/restore/upload', { file, timeoutMs: 30 * 60_000 });
export const restoreBackup = (adminPassword: string, password: string) =>
  request<{ version: string; created: string; plugins: boolean }>('POST', '/system/restore', {
    body: { adminPassword, password },
    timeoutMs: 30 * 60_000,
  });
