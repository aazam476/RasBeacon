/*
 * The demo's stand-in for the router's input checks, so the demo can show the error states. They
 * follow the original UI's shared/validation.ts and the checks in its hooks; the real provider
 * should use those (and the router re-checks everything anyway).
 */
import type { GuestSettings, HotspotSettings, NewNetworkForm, SavedNetwork, SetupForm } from '../types';

type Errors = Record<string, string>;

function checkSsid(ssid: string): string | null {
  const bytes = new TextEncoder().encode(ssid).length;
  if (bytes < 1 || bytes > 32) {
    return 'Must be 1 to 32 bytes long (emoji and accented letters count as several bytes).';
  }
  return null;
}

export function checkWifiPassword(password: string): string | null {
  if (password.length < 8 || password.length > 63) return 'Must be 8 to 63 characters long.';
  if (!/^[\x20-\x7e]*$/.test(password)) {
    return 'May only contain printable ASCII characters (no emoji or accented letters).';
  }
  return null;
}

export function checkSetupForm(form: SetupForm): Errors {
  const errors: Errors = {};
  if ([...form.adminPassword].length < 10) errors.adminPassword = 'Must be at least 10 characters long.';
  else if (form.adminPassword.toLowerCase().includes('rasbeacon')) {
    errors.adminPassword = 'Must not contain the device name.';
  }
  if (form.adminPassword !== form.adminPasswordConfirm) {
    errors.adminPasswordConfirm = "The two passwords don't match.";
  }
  const ssid = checkSsid(form.hotspotSsid);
  if (ssid) errors.hotspotSsid = ssid;
  const wifi = checkWifiPassword(form.hotspotPassword);
  if (wifi) errors.hotspotPassword = wifi;
  return errors;
}

export function checkHotspotSettings(settings: HotspotSettings): Errors {
  const errors: Errors = {};
  const ssid = checkSsid(settings.ssid);
  if (ssid) errors.ssid = ssid;
  if (settings.password) {
    const password = checkWifiPassword(settings.password);
    if (password) errors.password = password;
  }
  if (!/^[A-Z]{2}$/.test(settings.country)) errors.country = 'Enter a two-letter country code, e.g. US or DE.';
  return errors;
}

export function checkGuestSettings(settings: GuestSettings): Errors {
  const errors: Errors = {};
  if (!settings.enabled) return errors;
  const ssid = checkSsid(settings.ssid);
  if (ssid) errors.ssid = ssid;
  if (settings.password) {
    const password = checkWifiPassword(settings.password);
    if (password) errors.password = password;
  } else if (!settings.hasPassword) {
    errors.password = 'Choose a password for the guest network.';
  }
  return errors;
}

const SERVER_NAME = /^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$/i;

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

/** The router refuses WireGuard files that would run commands. */
export function checkWireGuardFile(text: string): string | null {
  const lines = text.split('\n');
  const index = lines.findIndex((line) => /^\s*(PreUp|PostUp|PreDown|PostDown)\s*=/i.test(line));
  if (index >= 0) {
    const key = lines[index].split('=')[0].trim();
    return `Line ${index + 1}: RasBeacon never runs commands from WireGuard files (${key}). Remove that line.`;
  }
  if (!/^\s*\[Interface\]/im.test(text) || !/^\s*\[Peer\]/im.test(text)) {
    return 'This is not a WireGuard file: it needs an [Interface] and a [Peer] section.';
  }
  return null;
}
