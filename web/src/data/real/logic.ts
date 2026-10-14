/**
 * The real provider's pure logic, kept apart from React so it can be unit-tested
 * (logic.test.ts). Most of it comes from the original UI: decidePhase (app/useApp.ts), the form
 * checks (shared/validation.ts, Setup/setupForm.ts, Guest/useGuestSettings.ts), the network list
 * helpers (Uplink/networkList.ts), the VPN form (VPN/vpnForm.ts) and the gestures on the login
 * page picture (Portal/browserGestures.ts).
 *
 * These checks mirror the router's (internal/config/config.go, internal/auth/password.go), so
 * mistakes show while typing. The router checks everything again; if the two ever disagree, the
 * router wins.
 */

import type { AppPhase } from '../device';
import { FieldErrors } from '../errors';
import type {
  GuestSettings,
  NewNetworkForm,
  PictureBox,
  PointerSpot,
  SavedNetwork,
  SetupForm,
  SetupState,
  VPNDetailsForm,
  PluginChoices,
} from '../types';
import { ApiError, type BrowserInput, type VPNProfileDetails } from './api';

// --- The phase -----------------------------------------------------------------------------------

/**
 * What fills the screen, from GET /setup and GET /session. `loggedIn` is undefined until the
 * session is known.
 */
export function decidePhase(
  setup: SetupState | undefined,
  loggedIn: boolean | undefined,
  error: string | null,
): AppPhase {
  if (setup === undefined || loggedIn === undefined) {
    return error ? { kind: 'unreachable', error } : { kind: 'loading' };
  }
  switch (setup.mode) {
    case 'setup':
      return { kind: 'setup' };
    case 'setup-closed':
    case 'setup-error':
    case 'no-radio':
      // After setup these can't happen, but a logged-in owner should still reach the settings,
      // so only an unclaimed router is blocked.
      if (!setup.claimed) return { kind: 'notice', notice: setup.mode };
      break;
  }
  return loggedIn ? { kind: 'ready' } : { kind: 'login' };
}

// --- Input checks --------------------------------------------------------------------------------

/** Wi-Fi network name: 1 to 32 bytes, no control characters. */
export function checkSsid(ssid: string): string | null {
  const bytes = new TextEncoder().encode(ssid).length;
  if (bytes < 1 || bytes > 32) {
    return 'Must be 1 to 32 bytes long (emoji and accented letters count as several bytes).';
  }
  // oxlint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(ssid)) return 'Must not contain control characters.';
  return null;
}

/** Wi-Fi (WPA2/WPA3) password: 8 to 63 printable ASCII characters. */
export function checkWifiPassword(password: string): string | null {
  if (password.length < 8 || password.length > 63) return 'Must be 8 to 63 characters long.';
  if (!/^[\x20-\x7e]*$/.test(password)) {
    return 'May only contain printable ASCII characters (no emoji or accented letters).';
  }
  return null;
}

/** The shortest admin password the router accepts. */
export const MIN_ADMIN_PASSWORD_LENGTH = 10;

/** Admin password: at least 10 characters. The router also refuses very common passwords. */
export function checkAdminPassword(password: string): string | null {
  if ([...password].length < MIN_ADMIN_PASSWORD_LENGTH) {
    return `Must be at least ${MIN_ADMIN_PASSWORD_LENGTH} characters long.`;
  }
  if (password.toLowerCase().includes('rasbeacon')) return 'Must not contain the device name.';
  return null;
}

type Errors = Record<string, string>;

/** Keeps the problems that were found (null means fine). */
function found(entries: Record<string, string | null>): Errors {
  const errors: Errors = {};
  for (const [key, problem] of Object.entries(entries)) if (problem) errors[key] = problem;
  return errors;
}

/** The setup form. Keys: adminPassword, adminPasswordConfirm, hotspotSsid, hotspotPassword. */
export function checkSetupForm(form: SetupForm): Errors {
  return found({
    adminPassword: checkAdminPassword(form.adminPassword),
    adminPasswordConfirm: form.adminPassword === form.adminPasswordConfirm ? null : "The two passwords don't match.",
    hotspotSsid: checkSsid(form.hotspotSsid),
    hotspotPassword: checkWifiPassword(form.hotspotPassword),
  });
}

/** The guest network form. Keys: ssid, password. */
export function checkGuestForm(settings: GuestSettings): Errors {
  if (!settings.enabled) return {};
  let password: string | null = null;
  if (settings.password) password = checkWifiPassword(settings.password);
  else if (!settings.hasPassword) password = 'Choose a password for the guest network.';
  return found({ ssid: checkSsid(settings.ssid), password });
}

/** Server names like "radius.example.edu" or "*.example.edu". */
const SERVER_NAME = /^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$/i;

/**
 * The "add a network" form. Keys: ssid, password, identity, clientCert, clientKey, caCert,
 * serverName. `list` is the list as edited, to refuse a network that is already in it.
 */
export function checkNewNetwork(form: NewNetworkForm, list: SavedNetwork[]): Errors {
  const errors: Errors = {};
  const ssid = checkSsid(form.ssid);
  if (ssid) errors.ssid = ssid;
  else if (list.some((network) => network.ssid === form.ssid)) errors.ssid = 'This network is already in the list.';

  if (form.security === 'personal') {
    const password = checkWifiPassword(form.password);
    if (password) errors.password = password;
  }
  if (form.security === 'enterprise') {
    if (!form.identity) errors.identity = 'Enter your username.';
    if (form.method === 'tls') {
      if (!form.clientCertFile) errors.clientCert = 'Choose your personal certificate file.';
      if (!form.clientKeyFile) errors.clientKey = 'Choose the key file for your certificate.';
    } else if (!form.password) {
      errors.password = 'Enter your password.';
    }
    if (!form.allowNoCertificateCheck) {
      if (form.caCertFiles.length === 0) {
        errors.caCert = "Choose the network's CA certificate file (your institution publishes it).";
      }
      if (!form.serverName) errors.serverName = "Enter the login server's name.";
      else if (!SERVER_NAME.test(form.serverName.trim())) {
        errors.serverName = 'Must look like radius.example.edu or *.example.edu.';
      }
    }
  }
  return errors;
}

// --- Certificate and key files -------------------------------------------------------------------

/** A file for the API: PEM files as text, binary (DER) files as base64. */
export function fileToUpload(bytes: Uint8Array): string {
  const text = new TextDecoder('utf-8', { fatal: false }).decode(bytes);
  if (text.includes('-----BEGIN')) return text;
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

/**
 * One or more certificate files as one PEM bundle. Binary (DER) certificates are converted, so a
 * bundle can mix both kinds. Some networks need several CA certificates.
 */
export function certificatesToUpload(files: Uint8Array[]): string {
  return files
    .map((bytes) => {
      const content = fileToUpload(bytes);
      if (content.includes('-----BEGIN')) return content.trim() + '\n';
      const lines = content.match(/.{1,64}/g) ?? [];
      return ['-----BEGIN CERTIFICATE-----', ...lines, '-----END CERTIFICATE-----', ''].join('\n');
    })
    .join('');
}

async function readBytes(file: File): Promise<Uint8Array> {
  return new Uint8Array(await file.arrayBuffer());
}

/** Turns the checked "add a network" form into a list entry, with the chosen files read. */
export async function toSavedNetwork(form: NewNetworkForm): Promise<SavedNetwork> {
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
    const caCerts = await Promise.all(form.caCertFiles.map(readBytes));
    network.enterprise = {
      method: form.method,
      identity: form.identity,
      anonymousIdentity: form.anonymousIdentity,
      serverName: form.serverName.trim(),
      allowNoCertificateCheck: form.allowNoCertificateCheck,
      caCert: caCerts.length > 0 ? certificatesToUpload(caCerts) : '',
      caCertSummary: form.caCertFiles.map((file) => file.name).join(', '),
      clientCert: tls && form.clientCertFile ? fileToUpload(await readBytes(form.clientCertFile)) : '',
      clientCertSummary: tls ? (form.clientCertFile?.name ?? '') : '',
      clientKey: tls && form.clientKeyFile ? fileToUpload(await readBytes(form.clientKeyFile)) : '',
      hasClientKey: false,
    };
  }
  return network;
}

// --- VPN -----------------------------------------------------------------------------------------

/** Splits "a, b,c" into ["a", "b", "c"]. */
export function splitList(text: string): string[] {
  return text
    .split(/[,\s]+/)
    .map((item) => item.trim())
    .filter((item) => item !== '');
}

/**
 * The server-details form as the API's request for one profile. Numbers must be whole numbers
 * or empty (the default); keys are the form's field names.
 */
export function vpnDetailsRequest(profile: string, form: VPNDetailsForm): VPNProfileDetails {
  const number = (text: string, field: keyof VPNDetailsForm, message: string) => {
    const trimmed = text.trim();
    if (trimmed === '') return 0;
    if (!/^\d+$/.test(trimmed)) throw new FieldErrors({ [field]: message });
    return Number(trimmed);
  };
  return {
    name: profile,
    addresses: splitList(form.addresses),
    dns: splitList(form.dns),
    mtu: number(form.mtu, 'mtu', 'Must be a number, or empty for the default.'),
    peerPublicKey: form.peerPublicKey.trim(),
    endpoint: form.endpoint.trim(),
    allowedIps: splitList(form.allowedIps),
    persistentKeepalive: number(
      form.persistentKeepalive,
      'persistentKeepalive',
      'Must be a number of seconds, or empty for the default.',
    ),
    routedIpv6Prefix: form.routedIpv6Prefix.trim(),
    privateKey: form.privateKey.trim(),
    presharedKey: form.presharedKey.trim(),
    // Read-only; the router ignores them.
    publicKey: '',
    hasPresharedKey: false,
  };
}

// --- The login page picture ----------------------------------------------------------------------

/** The page's size in RasBeacon's browser, in CSS pixels. */
export interface PageSize {
  width: number;
  height: number;
}

/** A finger that moved less than this (screen pixels) tapped; more, it swiped. */
export const TAP_SLOP = 10;

/** Converts a spot on the picture to the same spot on the page (the picture is scaled). */
export function toPagePoint(spot: PointerSpot, box: PictureBox, page: PageSize): { x: number; y: number } {
  const scaleX = box.width > 0 ? page.width / box.width : 1;
  const scaleY = box.height > 0 ? page.height / box.height : 1;
  const clamp = (value: number, high: number) => Math.round(Math.max(0, Math.min(high, value)));
  return {
    x: clamp((spot.clientX - box.left) * scaleX, page.width),
    y: clamp((spot.clientY - box.top) * scaleY, page.height),
  };
}

/**
 * What a pointer going down at `start` and up at `end` meant: a tap (click there) or a swipe
 * (scroll the page; swiping up scrolls down, as on any phone).
 */
export function classifyGesture(start: PointerSpot, end: PointerSpot, box: PictureBox, page: PageSize): BrowserInput {
  const at = toPagePoint(start, box, page);
  const movedY = end.clientY - start.clientY;
  const movedX = end.clientX - start.clientX;
  if (Math.abs(movedY) < TAP_SLOP && Math.abs(movedX) < TAP_SLOP) return { action: 'click', ...at };
  const scaleY = box.height > 0 ? page.height / box.height : 1;
  return { action: 'scroll', ...at, deltaY: Math.round(-movedY * scaleY) };
}

/** The page size to ask for: this screen's width, and most of its height. */
export function pageSizeFor(screenWidth: number, screenHeight: number): PageSize {
  return { width: Math.round(screenWidth), height: Math.round(screenHeight * 0.7) };
}

// --- Errors --------------------------------------------------------------------------------------

/**
 * Turns the router's refusal into what the screens show: a message next to an input when the
 * router named one the screen knows (`fields` maps the router's field names to the screen's
 * keys), otherwise a plain message.
 */
export function screenError(error: unknown, fields: Record<string, string> = {}): Error {
  if (error instanceof ApiError && error.field) {
    const key = fields[error.field] ?? networkErrorKey(error.field);
    if (key) return new FieldErrors({ [key]: error.message });
  }
  if (error instanceof Error) return error;
  return new Error(String(error));
}

/**
 * The router names a refused saved network's input like "networks.2.password" or
 * "networks.0.enterprise.caCert"; the Internet page shows it at the list position, "networks.2".
 */
export function networkErrorKey(field: string): string | null {
  const match = /^networks\.(\d+)(\.[\w.]+)?$/.exec(field);
  return match ? `networks.${match[1]}` : null;
}

/** Field names of the forms whose router names are the same as the screen's keys. */
export function sameNames(...keys: string[]): Record<string, string> {
  return Object.fromEntries(keys.map((key) => [key, key]));
}

/** The field keys of a plugin's form (ports.<key>, settings.<key>, drive), as the router names them. */
export function pluginFields(choices: PluginChoices): Record<string, string> {
  return sameNames(
    'drive',
    ...Object.keys(choices.ports).map((key) => `ports.${key}`),
    ...Object.keys(choices.settings).map((key) => `settings.${key}`),
  );
}
