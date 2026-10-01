// The API client: one function per API route.
//
// Screens call these functions instead of using fetch() directly, so all
// details (URLs, JSON, the CSRF header, error handling) live in one place.

import type {
  ClaimRequest,
  ClaimResponse,
  ErrorResponse,
  GuestSettings,
  HotspotSettings,
  HTTPSSettings,
  PortalBrowserFrame,
  PortalBrowserInput,
  PortalBrowserStart,
  PortalStatus,
  RevealPasswordResponse,
  ScanResponse,
  SessionInfo,
  SetupState,
  SSHSettings,
  Status,
  UpdateStatus,
  UplinkSettings,
  VPNSettings,
} from "./types";

/**
 * Thrown when the API answers with an error. `field` names the rejected
 * input (for example "hotspotPassword") so a form can show the message next
 * to it.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly field?: string;

  constructor(status: number, message: string, field?: string) {
    super(message);
    this.status = status;
    this.field = field;
  }
}

// The CSRF token from the last login. It must be sent with every request
// that changes something (see internal/webserver/sessions.go for why).
let csrfToken = "";

/** Sends one request and returns the parsed JSON response (or undefined for 204). */
async function request<T>(method: string, path: string, body?: unknown, file?: Blob): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (file !== undefined) {
    headers["Content-Type"] = "application/octet-stream";
  }
  if (method !== "GET" && csrfToken) {
    headers["X-CSRF-Token"] = csrfToken;
  }

  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: file ?? (body === undefined ? undefined : JSON.stringify(body)),
      credentials: "same-origin",
    });
  } catch {
    // fetch() only throws when there is no answer at all, for example after
    // the phone left the hotspot.
    throw new ApiError(0, "Can't reach RasBeacon. Check that this device is connected to it.");
  }

  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    let field: string | undefined;
    try {
      const error = (await response.json()) as ErrorResponse;
      message = error.error;
      field = error.field;
    } catch {
      // Not JSON: keep the generic message.
    }
    throw new ApiError(response.status, message, field);
  }

  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

/** Remembers the CSRF token from a session response. */
function rememberSession(session: SessionInfo): SessionInfo {
  csrfToken = session.csrfToken ?? "";
  return session;
}

// --- Public (no login needed) ---

export function getSetupState(): Promise<SetupState> {
  return request("GET", "/setup");
}

export async function claim(req: ClaimRequest): Promise<ClaimResponse> {
  const response = await request<ClaimResponse>("POST", "/setup/claim", req);
  // Claiming also logs us in; fetch the CSRF token for later requests.
  await getSession();
  return response;
}

export async function getSession(): Promise<SessionInfo> {
  return rememberSession(await request<SessionInfo>("GET", "/session"));
}

export async function login(password: string): Promise<SessionInfo> {
  return rememberSession(await request<SessionInfo>("POST", "/session", { password }));
}

export async function logout(): Promise<void> {
  await request<void>("DELETE", "/session");
  csrfToken = "";
}

// --- Login required ---

export function getStatus(): Promise<Status> {
  return request("GET", "/status");
}

export function getHotspotSettings(): Promise<HotspotSettings> {
  return request("GET", "/settings/hotspot");
}

export function updateHotspotSettings(settings: HotspotSettings): Promise<HotspotSettings> {
  return request("PUT", "/settings/hotspot", settings);
}

export function revealHotspotPassword(adminPassword: string): Promise<RevealPasswordResponse> {
  return request("POST", "/settings/hotspot/reveal-password", { password: adminPassword });
}

export function getGuestSettings(): Promise<GuestSettings> {
  return request("GET", "/settings/guest");
}

export function updateGuestSettings(settings: GuestSettings): Promise<GuestSettings> {
  return request("PUT", "/settings/guest", settings);
}

export function getSSHSettings(): Promise<SSHSettings> {
  return request("GET", "/settings/ssh");
}

export function updateSSHSettings(settings: SSHSettings): Promise<SSHSettings> {
  return request("PUT", "/settings/ssh", settings);
}

export function getUplinkSettings(): Promise<UplinkSettings> {
  return request("GET", "/settings/uplink");
}

export function updateUplinkSettings(settings: UplinkSettings): Promise<UplinkSettings> {
  return request("PUT", "/settings/uplink", settings);
}

/** Looks for nearby Wi-Fi networks. Takes about 5 seconds. */
export function scanNearbyNetworks(): Promise<ScanResponse> {
  return request("POST", "/uplink/scan");
}

export function getVPNSettings(): Promise<VPNSettings> {
  return request("GET", "/settings/vpn");
}

export function updateVPNSettings(settings: VPNSettings): Promise<VPNSettings> {
  return request("PUT", "/settings/vpn", settings);
}

/** Sends a WireGuard .conf file's text; the server checks and saves it. */
export function importVPNConfig(conf: string): Promise<VPNSettings> {
  return request("POST", "/settings/vpn/import", { conf });
}

/** Makes a new key pair for RasBeacon; the public key is in the answer. */
export function generateVPNKey(): Promise<VPNSettings> {
  return request("POST", "/settings/vpn/generate-key");
}

export function getHTTPSSettings(): Promise<HTTPSSettings> {
  return request("GET", "/settings/https");
}

export function updateHTTPSSettings(settings: HTTPSSettings): Promise<HTTPSSettings> {
  return request("PUT", "/settings/https", settings);
}

// --- Captive portal login mode (no login needed) ---

export function getPortalStatus(): Promise<PortalStatus> {
  return request("GET", "/portal");
}

/**
 * Starts login mode, extends it by 10 minutes, or stops it; "check" checks
 * for a login page again right away.
 */
export function portalAction(action: "start" | "extend" | "stop" | "check"): Promise<PortalStatus> {
  // An empty JSON body: rbweb only accepts JSON for these public actions.
  return request("POST", `/portal/${action}`, {});
}

// --- Zero-leak login: RasBeacon's own browser (no login needed) ---

/** Opens the login page in RasBeacon's browser, laid out for this screen size. */
export function startPortalBrowser(size: PortalBrowserStart): Promise<PortalStatus> {
  return request("POST", "/portal/browser/start", size);
}

export function stopPortalBrowser(): Promise<PortalStatus> {
  return request("POST", "/portal/browser/stop", {});
}

/**
 * Waits (up to about 10 seconds) for a picture newer than `after`, then
 * returns the newest one.
 */
export function getPortalBrowserFrame(after: number): Promise<PortalBrowserFrame> {
  return request("GET", `/portal/browser/frame?after=${after}`);
}

export function sendPortalBrowserInput(input: PortalBrowserInput): Promise<void> {
  return request("POST", "/portal/browser/input", input);
}

/**
 * Uploads an update file (.rbupdate). RasBeacon checks its signature,
 * installs it, then restarts. Takes a few minutes.
 */
export function installUpdate(file: File): Promise<UpdateStatus> {
  return request("POST", "/system/update", undefined, file);
}

/** Turns the online check for new releases on or off. */
export function setCheckOnline(checkOnline: boolean): Promise<UpdateStatus> {
  return request("PUT", "/settings/updates", { checkOnline });
}

export function reboot(): Promise<void> {
  return request("POST", "/system/reboot");
}

export function factoryReset(adminPassword: string): Promise<void> {
  return request("POST", "/system/factory-reset", { password: adminPassword });
}

/** The diagnostics report is a download, so the UI links to this URL directly. */
export const diagnosticsUrl = "/api/v1/diagnostics";
