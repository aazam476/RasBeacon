// Input checks that run in the browser, so mistakes are shown immediately
// while typing instead of after pressing Save.
//
// These mirror the checks in Go (internal/config/config.go and
// internal/auth/password.go). The device always re-checks everything, so
// these are only for convenience; if the two ever disagree, the device wins.
//
// Each function returns an error message, or null when the value is fine.

/** Wi-Fi network name: 1 to 32 bytes, no control characters. */
export function checkSsid(ssid: string): string | null {
  const bytes = new TextEncoder().encode(ssid).length;
  if (bytes < 1 || bytes > 32) {
    return "Must be 1 to 32 bytes long (emoji and accented letters count as several bytes).";
  }
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(ssid)) {
    return "Must not contain control characters.";
  }
  return null;
}

/** Wi-Fi (WPA2/WPA3) password: 8 to 63 printable ASCII characters. */
export function checkWifiPassword(password: string): string | null {
  if (password.length < 8 || password.length > 63) {
    return "Must be 8 to 63 characters long.";
  }
  if (!/^[\x20-\x7e]*$/.test(password)) {
    return "May only contain printable ASCII characters (no emoji or accented letters).";
  }
  return null;
}

/** The shortest admin password the device accepts. */
export const MIN_ADMIN_PASSWORD_LENGTH = 10;

/**
 * Admin password: at least 10 characters. The device also rejects very
 * common passwords; that list lives on the device only.
 */
export function checkAdminPassword(password: string): string | null {
  if ([...password].length < MIN_ADMIN_PASSWORD_LENGTH) {
    return `Must be at least ${MIN_ADMIN_PASSWORD_LENGTH} characters long.`;
  }
  if (password.toLowerCase().includes("rasbeacon")) {
    return "Must not contain the device name.";
  }
  return null;
}

/** Checks that the "confirm password" box matches. */
export function checkPasswordsMatch(password: string, confirmation: string): string | null {
  return password === confirmation ? null : "The two passwords don't match.";
}

/** Channels offered per band; must match allowedChannels in Go. */
export const CHANNELS = {
  "2.4GHz": [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11],
  "5GHz": [36, 40, 44, 48, 149, 153, 157, 161, 165],
} as const;
