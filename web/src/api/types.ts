// TypeScript versions of the API messages.
//
// These must match the Go types in internal/api/types.go exactly (same field
// names and meanings). docs/api.md lists which endpoint uses which type.
// If you change one side, change the other.

/** What the device is doing right now. */
export type Mode =
  | "setup" // first boot: waiting for the owner to finish setup
  | "setup-closed" // nobody finished setup within 30 minutes
  | "setup-error" // setup.txt exists but can't be used
  | "normal" // setup is done, hotspot is running
  | "no-radio"; // no Wi-Fi radio that can run a hotspot

export type Band = "2.4GHz" | "5GHz";

/** "wpa2-wpa3" lets new devices use WPA3; "wpa2" is for old devices. */
export type Security = "wpa2-wpa3" | "wpa2";

/** Returned with any error status. `field` names the input that was rejected. */
export interface ErrorResponse {
  error: string;
  field?: string;
}

/** GET /api/v1/setup (no login needed). */
export interface SetupState {
  mode: Mode;
  claimed: boolean;
  setupNetworkSecured: boolean;
  deviceShortId: string;
  lanAddress: string;
  suggestedHotspotSsid?: string;
  suggestedHotspotPassword?: string;
  setupClosesInSeconds?: number;
}

/** POST /api/v1/setup/claim */
export interface ClaimRequest {
  adminPassword: string;
  hotspotSsid: string;
  hotspotPassword: string;
  hotspotBand: Band;
}

export interface ClaimResponse {
  hotspotSsid: string;
  switchInSeconds: number;
  adminUrl: string;
}

/** Body for login and for actions that ask for the admin password again. */
export interface PasswordRequest {
  password: string;
}

/** GET/POST /api/v1/session */
export interface SessionInfo {
  loggedIn: boolean;
  csrfToken?: string;
}

/** One Wi-Fi radio found in the device. */
export interface Radio {
  interface: string;
  phy: string;
  driver: string;
  isUsb: boolean;
  usbId: string;
  supportsAp: boolean;
  role: "hotspot" | "uplink" | "unused";
}

/** One device connected to the hotspot. */
export interface Client {
  mac: string;
  ip: string;
  hostname: string;
  signalDbm: number;
  connectedSeconds: number;
}

export interface HotspotStatus {
  running: boolean;
  ssid: string;
  band: Band;
  channel: number;
  security: Security;
  interface: string;
  lanAddress: string;
}

/** What the Wi-Fi uplink radio is doing. */
export type WiFiUplinkState = "connected" | "connecting" | "searching" | "not-configured" | "off";

export interface WiFiUplinkStatus {
  /** False when there is no second radio for joining Wi-Fi networks. */
  available: boolean;
  interface: string;
  state: WiFiUplinkState;
  ssid: string;
  band: string;
  signalDbm: number;
}

export type EthernetRole = "uplink" | "client-port";

export interface EthernetUplinkStatus {
  present: boolean;
  role: EthernetRole;
  cablePlugged: boolean;
  /** Set when the port is a client port but a DHCP server answered on it. */
  foreignDhcpServer?: string;
}

/** RasBeacon's connection to the internet. */
export interface UplinkStatus {
  connected: boolean;
  /** Which uplink is in use, or "" when not connected. */
  kind: "ethernet" | "wifi" | "";
  interface: string;
  address: string;
  gateway: string;
  /** True when devices get IPv6 internet access. */
  ipv6: boolean;
  wifi: WiFiUplinkStatus;
  ethernet: EthernetUplinkStatus;
}

/** GET /api/v1/status */
export interface Status {
  version: string;
  boardModel: string;
  deviceId: string;
  uptimeSeconds: number;
  mode: Mode;
  hotspot: HotspotStatus;
  uplink: UplinkStatus;
  radios: Radio[];
  clients: Client[];
  warnings: string[];
}

/**
 * GET/PUT /api/v1/settings/hotspot. `password` is never returned; in a PUT,
 * leave it empty to keep the current password.
 */
export interface HotspotSettings {
  ssid: string;
  band: Band;
  channel: number; // 0 = default channel for the band
  security: Security;
  country: string;
  password?: string;
}

export interface RevealPasswordResponse {
  hotspotPassword: string;
}

/** GET/PUT /api/v1/settings/ssh */
export interface SSHSettings {
  enabled: boolean;
  authorizedKeys: string;
}

export type UplinkSecurity = "open" | "personal";

/**
 * One saved upstream Wi-Fi network. `password` is never returned; in a PUT,
 * leave it empty to keep the saved password. `id` is "" for a new network.
 */
export interface SavedNetwork {
  id: string;
  ssid: string;
  security: UplinkSecurity;
  hidden: boolean;
  hasPassword: boolean;
  password?: string;
}

/** GET/PUT /api/v1/settings/uplink */
export interface UplinkSettings {
  ethernetRole: EthernetRole;
  /** Most preferred first. */
  networks: SavedNetwork[];
  /** Read-only hints (ignored in a PUT). */
  ethernetPresent: boolean;
  wifiUplinkAvailable: boolean;
}

/** A Wi-Fi network found by a scan. */
export interface NearbyNetwork {
  ssid: string;
  signalDbm: number;
  frequencyMhz: number;
  /** "enterprise" (e.g. eduroam) arrives in milestone 3; "other" is not supported. */
  security: "open" | "personal" | "enterprise" | "other";
}

/** POST /api/v1/uplink/scan */
export interface ScanResponse {
  networks: NearbyNetwork[];
}
