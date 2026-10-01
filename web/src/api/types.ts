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
  /** True while devices are shown the "this network needs a login" notice. */
  portalNotice?: boolean;
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
  /** How many networks the radio can broadcast at once (2+ for the guest network). */
  maxAps: number;
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
  /** Devices are told the connection is limited. */
  metered: boolean;
}

/** The guest network on the status page. */
export interface GuestStatus {
  enabled: boolean;
  /** Being broadcast right now. */
  running: boolean;
  ssid: string;
  clients: Client[];
}

/**
 * GET/PUT /api/v1/settings/guest. `password` is never returned; in a PUT,
 * leave it empty to keep the saved one.
 */
export interface GuestSettings {
  enabled: boolean;
  ssid: string;
  password?: string;
  hasPassword: boolean;
  /** False when the Wi-Fi adapter can only broadcast one network (read-only). */
  supported: boolean;
}

/** What the Wi-Fi uplink radio is doing. */
export type WiFiUplinkState =
  "connected" | "connecting" | "searching" | "login-failed" | "not-configured" | "off";

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

/** Software updates (part of Status). */
export interface UpdateStatus {
  board: string;
  /** False when the image has no update signing key. */
  canInstall: boolean;
  installing: boolean;
  /** The last update's result, e.g. "Updated to v1.2.0." */
  message?: string;
  /** The optional online check for new releases. */
  canCheckOnline: boolean;
  checkOnline: boolean;
  latestVersion?: string;
  latestUrl?: string;
}

/**
 * GET /api/v1/portal (no login needed). A "captive portal" is the login page
 * of a hotel or café network.
 */
export interface PortalStatus {
  detected: boolean;
  loginUrl?: string;
  /** Login mode can be started (a login page blocks the VPN). */
  canStart: boolean;
  /** Devices may reach the login page directly right now. */
  loginMode: boolean;
  secondsLeft: number;
  /** The login page can be opened in RasBeacon's own browser (zero-leak login). */
  canBrowse: boolean;
  /** RasBeacon's browser is open. */
  browsing: boolean;
}

/** POST /api/v1/portal/browser/start: the phone screen's size in CSS pixels. */
export interface PortalBrowserStart {
  width: number;
  height: number;
}

/** GET /api/v1/portal/browser/frame?after=<seq>: a picture of the login page. */
export interface PortalBrowserFrame {
  /** False when the browser is closed (the other fields are then empty). */
  running: boolean;
  seq: number;
  /** Base64 JPEG, width x height. */
  image: string;
  width: number;
  height: number;
  url: string;
  title: string;
}

/** POST /api/v1/portal/browser/input: something the owner did on the picture. */
export type PortalBrowserInput =
  | { action: "click"; x: number; y: number }
  | { action: "scroll"; x: number; y: number; deltaY: number }
  | { action: "type"; text: string }
  | { action: "key"; key: "Enter" | "Backspace" | "Tab" | "Escape" }
  | { action: "back" }
  | { action: "reload" }
  | { action: "navigate"; url: string };

/** Where the WireGuard tunnel is (see internal/tunnel in the Go code). */
export type VPNState = "off" | "connecting" | "up" | "down" | "direct";

export interface VPNStatus {
  enabled: boolean;
  killSwitch: boolean;
  state: VPNState;
  /** Seconds since the last handshake with the server; -1 = never. */
  handshakeAgeSeconds: number;
  endpoint: string;
  receivedBytes: number;
  sentBytes: number;
  /** The public IPv6 network devices get addresses from, while in use. */
  routedIpv6Prefix?: string;
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
  vpn: VPNStatus;
  portal: PortalStatus;
  update: UpdateStatus;
  radios: Radio[];
  clients: Client[];
  guest: GuestStatus;
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
  /** Tell devices the connection is limited (Android reads it automatically). */
  metered: boolean;
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

export type UplinkSecurity = "open" | "personal" | "enterprise";

/** How RasBeacon logs in to an Enterprise network. */
export type EAPMethod = "ttls-pap" | "peap-mschapv2" | "tls";

/**
 * Login settings of an Enterprise network. Certificates and the key are
 * uploaded as PEM text or base64-encoded DER and are write-only (empty =
 * keep the saved file); the summaries describe what is saved.
 */
export interface EnterpriseSettings {
  method: EAPMethod;
  identity: string;
  anonymousIdentity: string;
  /** As the institution publishes it, e.g. "*.ruw.rutgers.edu". */
  serverName: string;
  /** Turns off checking the login server. Unsafe; the UI must warn. */
  allowNoCertificateCheck: boolean;
  caCert?: string;
  caCertSummary: string;
  clientCert?: string;
  clientCertSummary: string;
  clientKey?: string;
  hasClientKey: boolean;
}

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
  /** Network password, Enterprise password, or EAP-TLS key passphrase. */
  password?: string;
  enterprise?: EnterpriseSettings;
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
  /** "other" (WEP, Enhanced Open) is not supported. */
  security: "open" | "personal" | "enterprise" | "other";
  /** The exact kind, e.g. "WPA2/WPA3-Enterprise". */
  label: string;
}

/** POST /api/v1/uplink/scan */
export interface ScanResponse {
  networks: NearbyNetwork[];
}

/**
 * GET/PUT /api/v1/settings/vpn. Addresses are text, e.g. "10.66.0.2/32".
 * `privateKey` and `presharedKey` are write-only (empty = keep).
 */
export interface VPNSettings {
  enabled: boolean;
  killSwitch: boolean;
  /** A server has been set up (read-only). */
  configured: boolean;
  addresses: string[];
  dns: string[];
  mtu: number;
  peerPublicKey: string;
  endpoint: string;
  allowedIps: string[];
  persistentKeepalive: number;
  /** RasBeacon's public key, to add to the WireGuard server (read-only). */
  publicKey: string;
  privateKey?: string;
  presharedKey?: string;
  hasPresharedKey: boolean;
  removePresharedKey?: boolean;
  /** Optional public IPv6 /64 the server routes to RasBeacon; "" = off. */
  routedIpv6Prefix: string;
}

/**
 * GET/PUT /api/v1/settings/https. The certificate description is read-only;
 * generateNew, certificate and privateKey are write-only actions.
 */
export interface HTTPSSettings {
  enabled: boolean;
  uploaded: boolean;
  subject: string;
  /** SHA-256, as browsers show it (AB:CD:...). */
  fingerprint: string;
  validUntil: string;
  generateNew?: boolean;
  certificate?: string;
  privateKey?: string;
}
