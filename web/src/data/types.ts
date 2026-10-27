/**
 * Shapes of the data the UI displays.
 *
 * They are the router's API types (internal/api/types.go, docs/api.md) with the same names and
 * fields, so API responses are stored as they arrive.
 */

// --- Setup ----------------------------------------------------------------------------------

/** What the router is doing right now. */
export type Mode =
  | 'setup' // first boot: waiting for the owner to finish setup
  | 'setup-closed' // nobody finished setup within 30 minutes
  | 'setup-error' // setup.txt exists but can't be used
  | 'normal' // setup is done, the hotspot is running
  | 'no-radio'; // no Wi-Fi radio that can run a hotspot

export type Band = '2.4GHz' | '5GHz' | '6GHz';

/** "wpa2-wpa3" lets new devices use WPA3; "wpa2" is for old devices. */
export type Security = 'wpa2-wpa3' | 'wpa2' | 'wpa3';

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
  /** The hotspot's radio has no 5 GHz band, so setup offers only 2.4 GHz. */
  no5GHz?: boolean;
}

/** The setup form. The API's ClaimRequest is the same without `adminPasswordConfirm`. */
export interface SetupForm {
  adminPassword: string;
  adminPasswordConfirm: string;
  hotspotSsid: string;
  hotspotPassword: string;
  hotspotBand: Band;
  /**
   * Where RasBeacon will be used, which only changes the next steps "Setup complete" shows: on
   * trips it joins the place's Wi-Fi, at home its Ethernet port goes to the home router. Not sent
   * to the router (the Ethernet port is an uplink either way, and every setting stays
   * changeable).
   */
  place: SetupPlace;
}

/** "Where will you use RasBeacon?" in the setup. */
export type SetupPlace = 'travel' | 'home';

/** The form that changes the admin password (System page). */
export interface AdminPasswordForm {
  currentPassword: string;
  newPassword: string;
  newPasswordConfirm: string;
}

/** POST /api/v1/setup/claim answers with this. */
export interface ClaimResponse {
  hotspotSsid: string;
  switchInSeconds: number;
  adminUrl: string;
}

// --- Status (GET /api/v1/status) -------------------------------------------------------------

/** What a Wi-Fi radio does. */
export type RadioRole = 'hotspot' | 'uplink' | 'guest' | 'unused';

/** One Wi-Fi radio found in the router. */
export interface Radio {
  interface: string;
  /** The radio's own MAC address: how RasBeacon remembers it (names change). */
  mac: string;
  phy: string;
  driver: string;
  isUsb: boolean;
  usbId: string;
  supportsAp: boolean;
  /** How many networks the radio can broadcast at once (2+ for the guest network). */
  maxAps: number;
  role: RadioRole;
  /** The owner chose this role (System page); otherwise RasBeacon did. */
  chosen?: boolean;
  /** What it can run as a hotspot: Wi-Fi 6, 6 GHz (Wi-Fi 6E), Wi-Fi 7 (not used yet). */
  hasWifi6?: boolean;
  has6GHz?: boolean;
  hasWifi7?: boolean;
}

/** One device connected to the hotspot or the guest network. */
export interface Client {
  mac: string;
  /** "" while the device waits for an address. */
  ip: string;
  /** "" when the device didn't send a name; the screens show the MAC address instead. */
  hostname: string;
  signalDbm: number;
  connectedSeconds: number;

  // Live traffic and the per-device controls.
  /** "" for a device on the Ethernet cable. */
  band: Band | '';
  downMbps: number;
  upMbps: number;
  /** False while the owner has paused this device's internet access. */
  internetAccess: boolean;
  /** How long access has been paused, in seconds. */
  pausedSeconds?: number;
  /** The phone or laptop that is viewing this page (only the router can tell, by its address). */
  isThisDevice?: boolean;
  /**
   * On the Ethernet client port instead of the Wi-Fi: no band or signal, traffic isn't measured,
   * and it can't be disconnected from here (it can be paused).
   */
  wired?: boolean;
  /**
   * The welcome page: `waiting` until the device accepts it, then `accepted`; absent where there
   * is no welcome page.
   */
  welcome?: 'waiting' | 'accepted';
  /** The owner's name for the device; the screens prefer it over `hostname`. */
  name?: string;
  /** The address it always gets on the hotspot, when the owner chose one ("Always use this address"). */
  fixedAddress?: string;
  /** A priority device: its traffic goes first when the internet connection is busy. */
  priority?: boolean;
  /** Data used since the counters were reset (Wi-Fi devices; 0 for wired ones). */
  downBytes: number;
  upBytes: number;
  /** Paused for going over the data limit. */
  limitReached?: boolean;
}

export interface HotspotStatus {
  running: boolean;
  ssid: string;
  band: Band;
  /** The saved channel (or the band's default). */
  channel: number;
  /**
   * The channel the radio uses right now, when known. On 5 GHz it can differ from the saved one:
   * the radio may move within the same 80 MHz group (36 → 40) when a nearby network uses it.
   */
  actualChannel?: number;
  security: Security;
  interface: string;
  lanAddress: string;
  /** Devices are told the connection is limited. */
  metered: boolean;
  /** The Wi-Fi standard it runs. */
  standard?: WiFiStandard;
}

/** The guest network on the Status page. */
export interface GuestStatus {
  enabled: boolean;
  /** Being broadcast right now. */
  running: boolean;
  ssid: string;
  clients: Client[];
}

/** What the Wi-Fi uplink radio is doing. */
export type WiFiUplinkState = 'connected' | 'connecting' | 'searching' | 'login-failed' | 'not-configured' | 'off';

export interface WiFiUplinkStatus {
  /** False when there is no second radio for joining Wi-Fi networks. */
  available: boolean;
  interface: string;
  state: WiFiUplinkState;
  ssid: string;
  band: string;
  signalDbm: number;

  // Shown on the Internet page while connected; hidden when missing.
  bssid?: string;
  channel?: number;
  frequencyMhz?: number;
  /** The connection's Wi-Fi standard, channel width and current rates each way. */
  standard?: UplinkStandard;
  widthMhz?: number;
  rxMbps?: number;
  txMbps?: number;
}

/** The Wi-Fi standard of a connection or of a nearby network. */
export type UplinkStandard = 'wifi7' | 'wifi6e' | 'wifi6' | 'wifi5' | 'wifi4';

export type EthernetRole = 'uplink' | 'client-port';

export interface EthernetUplinkStatus {
  present: boolean;
  role: EthernetRole;
  cablePlugged: boolean;
  /** Set when the port is a client port but a DHCP server answered on it. */
  foreignDhcpServer?: string;

  // Shown while the cable carries the internet; hidden when missing.
  /** e.g. "1 Gbps". */
  linkSpeed?: string;
  /** e.g. "1000 Mbps Full Duplex". */
  duplex?: string;
}

/** RasBeacon's connection to the internet. */
export interface UplinkStatus {
  connected: boolean;
  /** Which uplink is in use, or "" when not connected. `phone`: a phone or modem on USB. */
  kind: 'ethernet' | 'wifi' | 'phone' | 'modem' | '';
  interface: string;
  address: string;
  gateway: string;
  /** True when devices get IPv6 internet access. */
  ipv6: boolean;
  wifi: WiFiUplinkStatus;
  ethernet: EthernetUplinkStatus;
  /** Phones sharing their connection and modems on USB; absent or null without one. */
  phones?: PhoneUplinkStatus[] | null;
  /** The mobile broadband modem that needs ModemManager; absent without one. */
  modem?: ModemStatus;

  // Hidden when missing.
  subnetMask?: string;
  /** MAC address of RasBeacon's own interface for this uplink. */
  interfaceMac?: string;
}

/** Software updates (part of Status). */
export interface UpdateStatus {
  board: string;
  /** False when the image has no update signing key. */
  canInstall: boolean;
  /** The Pi Zero 2 W image: one copy of the system, so new versions are flashed, not installed. */
  singleSystem?: boolean;
  installing: boolean;
  /** The last update's result, e.g. "Updated to v1.2.0." */
  message?: string;
  /** The optional online check for new releases. */
  canCheckOnline: boolean;
  /** How often it asks GitHub: 0 (only with Check now), 1, 6, 24 or 168 hours. */
  checkHours: number;
  /** When it last asked (ISO 8601), and why that failed. */
  lastChecked?: string;
  checkError?: string;
  /** The newest release this RasBeacon can install, and its release page. */
  latestVersion?: string;
  latestUrl?: string;
  /**
   * An even newer release that can only be installed over `latestVersion` (or a later one), such as
   * one signed with a new key. It is offered once `latestVersion` runs.
   */
  newerVersion?: string;
  /**
   * A newer version that can't be installed over this one: the online check found it, or its
   * update file was refused. The way to it is a backup, the new image, and the backup restored.
   */
  reflash?: UpdateReflash;
  /**
   * An installed update waits for its restart: the restart after installing failed (`message` says
   * why), or RasBeacon was switched off first. `restartIntoUpdate` finishes it.
   */
  restartPending?: boolean;
}

/** A new version that needs a fresh install (`UpdateStatus.reflash`). */
export interface UpdateReflash {
  version: string;
  /** The oldest version that can install its update file. */
  minFromVersion: string;
  /** Its release page, when known. */
  url?: string;
}

/** A "captive portal" is the login page of a hotel or café network. GET /api/v1/portal (no login). */
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

/** Where the WireGuard tunnel is. */
/** `blocked`: the VPN is off but the kill switch is on, so devices have no internet. */
export type VPNState = 'off' | 'connecting' | 'up' | 'down' | 'direct' | 'blocked';

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

/** Live traffic through the router. */
export interface Throughput {
  downMbps: number;
  downPeakMbps: number;
  upMbps: number;
  upPeakMbps: number;
  /** Highest combined rate seen this session. */
  peakMbps: number;
  /** Everything routed since the router started. */
  sessionBytes: number;
}

/** Hardware readings. */
export interface SystemStats {
  cpuPercent: number;
  cpuModel: string;
  ramUsedMb: number;
  ramTotalMb: number;
  swapUsedMb: number;
  swapTotalMb: number;
  temperatureC: number;
  thermalState: string;
  cooling: string;
  sensorStatus: string;
  throttling: string;
}

/** Identifiers shown on the System page. */
export interface DeviceInfo {
  hostname: string;
  modelSpec: string;
  kernel: string;
  kernelRelease: string;
  appliance: string;
  bootDevice: string;
}

/** The SD card or disk. */
export interface StorageInfo {
  totalGb: number;
  firmwareGb: number;
  userGb: number;
  medium: string;
  filesystem: string;
  health: string;
}

/** The hotspot's own network. */
export interface LanInfo {
  maxClients: number;
  subnet: string;
  dhcpRange: string;
}

/** Details of the Ethernet port while it carries the internet. */
export interface EthernetDiagnostics {
  carrierState: string;
  controller: string;
  mtu: string;
  packetStatistics: string;
  dnsServers: string;
  dhcpLease: string;
  portDescription: string;
  pollingInterval: string;
}

/** The result of Cloudflare's speed test (github.com/cloudflare/speedtest), in the units shown. */
export interface SpeedtestResult {
  /** The Cloudflare location tested against, e.g. "San Jose, CA (SJC)"; "" when unknown (not shown). */
  server: string;
  /** Seconds since the test ran. */
  ageSeconds: number;
  /** `getDownloadBandwidth()`, which is in bits per second, divided by 1,000,000. */
  downloadMbps: number;
  /** `getUploadBandwidth()` divided by 1,000,000. */
  uploadMbps: number;
  /** `getUnloadedLatency()`, in milliseconds. */
  pingMs: number;
}

/** The speed test. It only runs when the owner asks. */
export interface Speedtest {
  /** The test service, shown as "using …": "Cloudflare". */
  provider: string;
  /** A test is running (started from any page or device): the button waits for it. */
  running: boolean;
  /** null until the first test has run. */
  last: SpeedtestResult | null;
}

export interface Status {
  version: string;
  boardModel: string;
  deviceId: string;
  uptimeSeconds: number;
  mode: Mode;
  /** The installed plugins and their state. */
  plugins: PluginSummary[];
  hotspot: HotspotStatus;
  uplink: UplinkStatus;
  vpn: VPNStatus;
  portal: PortalStatus;
  update: UpdateStatus;
  radios: Radio[];
  /** Seconds left to keep new radio roles before RasBeacon goes back to the previous ones. */
  radioRolesPending?: number;
  /** Devices on the hotspot (the guest network's devices are in `guest.clients`). */
  clients: Client[];
  guest: GuestStatus;
  /** Plain sentences from the router, 0 to about 5; they can be long. */
  warnings: string[];

  throughput: Throughput;
  system: SystemStats;
  device: DeviceInfo;
  storage: StorageInfo;
  lan: LanInfo;
  speedtest: Speedtest;
  /** null while the cable doesn't carry the internet. */
  ethernetDiagnostics: EthernetDiagnostics | null;
  /** Extra wired ports for devices, such as a USB Ethernet adapter; empty without one. */
  lanPorts: LANPortStatus[];
}

// --- Settings --------------------------------------------------------------------------------

/**
 * GET/PUT /api/v1/settings/hotspot. `password` is never returned; when saving, empty keeps the
 * current password.
 */
export interface HotspotSettings {
  ssid: string;
  band: Band;
  /** 0 = the band's default channel. */
  channel: number;
  security: Security;
  /** Two-letter country code; it decides which channels are legal. */
  country: string;
  /** Tell devices the connection is limited (Android reads it automatically). */
  metered: boolean;
  password?: string;
  /** Read-only: the hotspot's radio has no 5 GHz band, so only 2.4 GHz is offered. */
  no5GHz?: boolean;
  /** Read-only: the hotspot's radio can't run a WPA3-only hotspot, so it isn't offered. */
  noWpa3?: boolean;
  /**
   * The Wi-Fi standard in use (the best the radio and band allow, unless one was chosen). Sending
   * the one in use keeps "the best".
   */
  standard?: WiFiStandard;
  /** Read-only: the hotspot's radio can't run Wi-Fi 6, so it isn't offered. */
  noWifi6?: boolean;
  /** Read-only: the hotspot's radio can run a 6 GHz (Wi-Fi 6E) hotspot, so the band is offered. */
  has6GHz?: boolean;
}

/** A Wi-Fi standard the hotspot can run: 802.11ax, 802.11ac (5 GHz only) or 802.11n. */
export type WiFiStandard = 'wifi6' | 'wifi5' | 'wifi4';

/**
 * GET/PUT /api/v1/settings/guest. `password` is never returned; when saving, empty keeps the
 * saved one.
 */
export interface GuestSettings {
  enabled: boolean;
  ssid: string;
  password?: string;
  hasPassword: boolean;
  /** False when the Wi-Fi adapter can only broadcast one network (read-only). */
  supported: boolean;
}

/** GET/PUT /api/v1/settings/access: which and how many devices may join the hotspot over Wi-Fi. */
export interface AccessSettings {
  /** Only the devices in `allowed` may join your network (the guest network isn't affected). */
  allowListOn: boolean;
  /** MAC addresses such as 3a:1f:22:aa:bb:cc. */
  allowed: string[];
  /** The most devices on each network at once; 0 for no limit (up to 231). */
  maxDevices: number;
}

/** GET/PUT /api/v1/settings/updates: how often to look for a new version, and where to announce it. */
export interface UpdateSettings {
  /** 0 (only with Check now), 1, 6, 24 or 168 hours. */
  checkHours: number;
  /** An ntfy topic or a webhook; "" for none. */
  notifyUrl: string;
}

/** GET/PUT /api/v1/settings/swap: the swap file on the SD card, in whole gigabytes. */
export interface SwapSettings {
  /** 0 = off. */
  sizeGb: number;
  /** The largest size that fits (the card's free space minus 1 GB); read-only. */
  maxGb: number;
}

/**
 * GET/PUT /api/v1/settings/dns: the DNS servers devices use while their traffic isn't going
 * through the VPN. Empty uses the current network's. Each is an IP address (plain DNS) or a
 * DNS-over-HTTPS address (https://dns.example/dns-query; a bare host name means that path).
 */
export interface DNSSettings {
  servers: string[];
  /** Use the servers while the VPN is on too (they are then reached through it). */
  withVpn: boolean;
}

/** Which connections a port forwarding rule passes on. */
export type PortForwardProtocol = 'tcp' | 'udp' | 'both';

/**
 * One port forwarding rule: connections from the internet to `outsidePort` (or the range up to
 * `outsidePortEnd`) on RasBeacon's outside address go to a device on the hotspot (`mac`, at its
 * fixed address) or to a plugin (`plugin`).
 */
export interface PortForwardRule {
  /** "" for a new rule; the router gives it one. */
  id: string;
  name: string;
  protocol: PortForwardProtocol;
  outsidePort: number;
  /** The last port of a range; absent (or 0) for one port. */
  outsidePortEnd?: number;
  /** The device's MAC address, or absent for a plugin. */
  mac?: string;
  /** The plugin's ID, or absent for a device. */
  plugin?: string;
  /** The port on the device; absent (or 0) for the same as outside. */
  insidePort?: number;
  /**
   * Also opens the port (the inside one) on the device's public IPv6 addresses, while the VPN
   * routes an IPv6 network to RasBeacon (`ipv6Prefix`). Devices only.
   */
  ipv6?: boolean;
  enabled: boolean;
  /** Read-only: the device's or plugin's name. */
  target?: string;
  /** Read-only: where the connections go; absent while the device has no fixed address. */
  address?: string;
  /** Read-only: a risk, such as a port often attacked. */
  warning?: string;
  /** Read-only, with `ipv6`: the device's public IPv6 addresses the port is open on. */
  ipv6Addresses?: string[];
}

/**
 * GET/PUT /api/v1/settings/port-forwarding. A PUT sends the whole list of rules; the router
 * answers with the list as saved.
 */
export interface PortForwardingSettings {
  rules: PortForwardRule[];
  /** Read-only: RasBeacon's address on the network that carries the internet; absent while offline. */
  outsideAddress?: string;
  /**
   * Read-only: whether the internet can reach that address. `private`: RasBeacon is behind
   * another router, which must forward the ports to it first. `shared`: the network shares one
   * address between many customers (carrier-grade NAT), so it can't be reached.
   */
  reachability?: 'public' | 'private' | 'shared' | 'offline';
  /** Read-only: the public IPv6 network the VPN routes to RasBeacon; absent when there is none. */
  ipv6Prefix?: string;
}

/**
 * GET/PUT /api/v1/settings/usage: a data limit per device that applies while the connection is
 * marked metered.
 */
export interface UsageSettings {
  /** 0 = no limit. */
  limitMb: number;
  /** When counting started (RFC 3339); read-only. */
  since?: string;
  /** How many devices the limit pauses now; read-only. */
  devicesPaused: number;
}

/**
 * GET/PUT /api/v1/settings/qos: the internet connection's speeds, which priority devices need.
 * RasBeacon sends at 90% of them so the queue forms where it can put priority devices first.
 */
export interface QoSSettings {
  /** Kilobits per second; 0 = not set (that direction isn't shaped). */
  uploadKbps: number;
  downloadKbps: number;
  /** How many priority devices there are; read-only. */
  priorityDevices: number;
  /** Priority devices' traffic is being put first now; read-only. */
  active: boolean;
  /** Why it isn't, when RasBeacon couldn't set up its queues; read-only. */
  problem?: string;
}

/** POST /api/v1/wifi-qr: a QR code phones scan to join a network. */
/** The channels a country allows a hotspot on, by band (POST /settings/hotspot/channels). */
export type ChannelsByBand = Record<Band, number[]>;

export interface HotspotChannelsResponse {
  channels: ChannelsByBand;
}

export interface WiFiQR {
  ssid: string;
  /** An SVG image, black on white. */
  svg: string;
}

/** GET/PUT /api/v1/settings/ssh. `authorizedKeys` is the text of the keys, one per line. */
export interface SSHSettings {
  enabled: boolean;
  authorizedKeys: string;
}

export type UplinkSecurity = 'open' | 'personal' | 'enterprise';

/** How RasBeacon logs in to an Enterprise network. */
export type EAPMethod = 'ttls-pap' | 'peap-mschapv2' | 'tls';

/**
 * Login settings of an Enterprise network. Certificates and the key are write-only; the
 * summaries describe what is saved, e.g. "Example Root CA (valid until 2031-04-01)".
 */
export interface EnterpriseSettings {
  method: EAPMethod;
  identity: string;
  anonymousIdentity: string;
  /** As the network publishes it, e.g. "*.example.com". Optional. */
  serverName: string;
  /** Turns off checking the login server. Unsafe; the UI warns. */
  allowNoCertificateCheck: boolean;
  /**
   * Without a CA certificate or server name: the SHA-256 of the login server's certificate,
   * trusted at the first connection (empty until then). Read only.
   */
  serverCertHash?: string;
  caCert?: string;
  caCertSummary: string;
  clientCert?: string;
  clientCertSummary: string;
  clientKey?: string;
  hasClientKey: boolean;
}

/**
 * One saved upstream Wi-Fi network. `password` is never returned; when saving, empty keeps the
 * saved password. `id` is "" for a network that isn't saved yet.
 */
export interface SavedNetwork {
  id: string;
  ssid: string;
  security: UplinkSecurity;
  hidden: boolean;
  /** Join with a random MAC address instead of the Pi's own (a new one at each restart). */
  randomMac?: boolean;
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
  /** Read-only hints. */
  ethernetPresent: boolean;
  wifiUplinkAvailable: boolean;
  /** Treat a phone or modem connection as metered while in use (absent: unchanged). */
  phoneMetered?: boolean;
  /** The MAC address the Wi-Fi uplink shows its networks (absent: unchanged). */
  wifiMac?: MACChoice;
  /** The MAC address the Ethernet port shows its network as the uplink (absent: unchanged). */
  ethernetMac?: MACChoice;
}

/**
 * The MAC address an uplink shows: the Pi's own (`mode` ""), a random one RasBeacon made once
 * ("random"; sent without an address it makes a new one), or another device's ("cloned").
 */
export interface MACChoice {
  mode?: '' | 'random' | 'cloned';
  /** Lowercase, like "3a:1f:22:aa:bb:cc"; absent for the Pi's own. */
  address?: string;
}

/** A Wi-Fi network found by a scan (POST /api/v1/uplink/scan). */
export interface NearbyNetwork {
  ssid: string;
  signalDbm: number;
  frequencyMhz: number;
  /** "other" (WEP, Enhanced Open) is not supported. */
  security: 'open' | 'personal' | 'enterprise' | 'other';
  /** The exact kind, e.g. "WPA2/WPA3-Enterprise". */
  label: string;
  /** The newest Wi-Fi standard it offers, when known. */
  standard?: UplinkStandard;
}

/**
 * What the "add a network" dialog sends to `prepareNetwork`. The chosen files are passed as
 * they are; turning them into what the API stores is the router side's job.
 */
export interface NewNetworkForm {
  ssid: string;
  security: UplinkSecurity;
  /** Network password; for Enterprise, your password or (TLS) the key passphrase. */
  password: string;
  hidden: boolean;
  // Enterprise only:
  method: EAPMethod;
  identity: string;
  anonymousIdentity: string;
  serverName: string;
  /** Several files allowed: some institutions publish more than one CA certificate. */
  caCertFiles: File[];
  clientCertFile: File | null;
  clientKeyFile: File | null;
  allowNoCertificateCheck: boolean;
}

/**
 * One WireGuard configuration (a profile): the server, the addresses and RasBeacon's public key.
 * The router keeps several and uses one.
 */
export interface VPNProfile {
  /** Unique; usually the imported file's name, e.g. "hotel-tunnel-01.conf". */
  name: string;
  addresses: string[];
  dns: string[];
  /** 0 = default (1380). */
  mtu: number;
  peerPublicKey: string;
  endpoint: string;
  allowedIps: string[];
  /** 0 = default (25). */
  persistentKeepalive: number;
  /** RasBeacon's public key for this profile, to add to the WireGuard server. */
  publicKey: string;
  hasPresharedKey: boolean;
  /** Optional public IPv6 /64 the server routes to RasBeacon; "" = off. */
  routedIpv6Prefix: string;
}

/** GET /api/v1/settings/vpn: the switches and the profiles. */
export interface VPNSettings {
  enabled: boolean;
  killSwitch: boolean;

  /** The profile in use; "" when there is none. */
  activeProfile: string;
  profiles: VPNProfile[];
}

/**
 * The server-details form. Lists are comma-separated text and numbers are text, exactly as
 * typed; empty keys and numbers mean "keep" or "default".
 */
export interface VPNDetailsForm {
  endpoint: string;
  peerPublicKey: string;
  addresses: string;
  dns: string;
  allowedIps: string;
  persistentKeepalive: string;
  mtu: string;
  routedIpv6Prefix: string;
  presharedKey: string;
  privateKey: string;
}

/** GET/PUT /api/v1/settings/https. The certificate description is read-only. */
export interface HTTPSSettings {
  enabled: boolean;
  uploaded: boolean;
  /** "" when there is no certificate yet. */
  subject: string;
  /** SHA-256, as browsers show it (AB:CD:...); "" when there is no certificate yet. */
  fingerprint: string;
  validUntil: string;
}

// --- The hotel login page in RasBeacon's own browser -------------------------------------------

/** A picture of the login page in RasBeacon's browser. */
export interface PortalBrowserFrame {
  /** False when the browser is closed. */
  running: boolean;
  seq: number;
  /** The page's size in CSS pixels; the picture has the same proportions. */
  width: number;
  height: number;
  url: string;
  title: string;
}

/** Where a finger or mouse went down or up on the picture, in screen pixels. */
export interface PointerSpot {
  clientX: number;
  clientY: number;
}

/** The picture's position and size on screen (its getBoundingClientRect()). */
export interface PictureBox {
  left: number;
  top: number;
  width: number;
  height: number;
}

export type PortalKey = 'Enter' | 'Backspace' | 'Tab';

// --- Plugins (docs/plugins.md) ------------------------------------------------------------------

/** A permission a plugin asks for, in plain words. */
export interface PluginPermission {
  id: string;
  title: string;
  detail: string;
}

/** One of a plugin's ports: `key` ("tcp/3000") names it in requests, `host` is the port on RasBeacon. */
export interface PluginPort {
  key: string;
  name: string;
  protocol: 'tcp' | 'udp';
  container: number;
  host: number;
  /** Where to open it, for a web page. */
  url?: string;
}

/** A field of a plugin's settings form. Passwords are never sent back: `hasValue` says one is saved. */
export interface PluginSetting {
  key: string;
  label: string;
  type: 'text' | 'password' | 'number' | 'switch' | 'select';
  help?: string;
  unit?: string;
  required?: boolean;
  options?: { value: string; label: string }[];
  min?: number;
  max?: number;
  value: string;
  hasValue?: boolean;
}

export type PluginState = 'installing' | 'waiting' | 'starting' | 'running' | 'stopped' | 'crashed' | 'failed';

/** An installed plugin (GET /api/v1/plugins). */
export interface Plugin {
  id: string;
  name: string;
  version: string;
  description: string;
  author?: string;
  homepage?: string;
  /** A data: URL. */
  icon: string;
  readme?: string;
  /** "file", or the catalog's "owner/repo". */
  source: string;
  signed: boolean;
  keyId?: string;
  state: PluginState;
  /** Progress, or what went wrong. */
  message?: string;
  healthy?: boolean;
  address: string;
  permissions: PluginPermission[];
  ports: PluginPort[];
  settings: PluginSetting[];
  usesDrive: boolean;
  driveOptional?: boolean;
  drive?: string;
  afterInstall?: string;
  /** Up to 4; an empty value can't be read right now. */
  metrics: { label: string; value: string }[];
  memoryMb: number;
  cpus: number;
  storageMb: number;
  /** A newer version in the catalog. */
  updateVersion?: string;
  /** The update installs from the catalog (signed plugins only; unsigned ones update from a file). */
  canUpdate: boolean;
  /** Its files can't be read: only removing it, or installing it again, helps. */
  broken?: boolean;
}

export interface PluginsState {
  plugins: Plugin[];
  sources: { source: string; builtIn: boolean }[];
  board: string;
  memoryMb: number;
  swapMb: number;
  metered: boolean;
  checkedAt?: string;
  checkError?: string;
  /** Data kept from removed plugins ("keep its data"), waiting for a later install. */
  kept: PluginKept[];
}

/** The data kept from a removed plugin: the space it takes on the card. */
export interface PluginKept {
  id: string;
  /** The plugin's name (its ID for data RasBeacon 0.1 kept). */
  name: string;
  bytes: number;
}

/** A plugin on the status page. */
export interface PluginSummary {
  id: string;
  name: string;
  state: PluginState;
  icon: string;
}

/** What the install sheet shows before installing. */
export interface PluginPreview {
  stagingId: string;
  id: string;
  name: string;
  version: string;
  description: string;
  author?: string;
  homepage?: string;
  icon: string;
  source: string;
  signed: boolean;
  keyId?: string;
  update: boolean;
  fromVersion?: string;
  permissions: PluginPermission[];
  /** Permissions an update adds. */
  newPermissions?: string[];
  ports: PluginPort[];
  settings: PluginSetting[];
  usesDrive: boolean;
  driveOptional?: boolean;
  drive?: string;
  /** What installing still downloads, in bytes (-1: unknown). */
  downloadSize: number;
  metered: boolean;
  memoryMb: number;
  cpus: number;
  storageMb: number;
  warnings?: string[];
  /** Set when data kept from an earlier install waits, but not from the same author. */
  keptData?: PluginKeptData;
}

/** The owner's choices in the install sheet or on a plugin's page. */
/**
 * Data kept from an earlier install with the same ID that doesn't come back by itself (it wasn't
 * signed with the same key): the install sheet asks. `name` is the earlier plugin's, `keyId` its
 * signing key; both are missing for data RasBeacon 0.1 kept, which didn't note them.
 */
export interface PluginKeptData {
  name?: string;
  keyId?: string;
}

export interface PluginChoices {
  /** By port key ("tcp/3000"). */
  ports: Record<string, number>;
  /** By setting key; an empty password keeps the saved one. */
  settings: Record<string, string>;
  /** The USB drive's UUID, "" for none. */
  drive: string;
}

export interface PluginCatalogEntry {
  source: string;
  file: string;
  id?: string;
  name: string;
  version?: string;
  description?: string;
  permissions?: string[];
  size: number;
  downloadSize?: number;
  icon?: string;
  installedVersion?: string;
  /** This version is newer than the installed one (the router compares them). */
  updateAvailable?: boolean;
  runsHere: boolean;
  /** Set when this RasBeacon is too old for it: the version it needs ("0.2.0"). */
  needsRasBeacon?: string;
}

export interface PluginCatalog {
  entries: PluginCatalogEntry[];
  checkedAt?: string;
  /** By source. */
  errors?: Record<string, string>;
}

export interface PluginDrive {
  uuid: string;
  label: string;
  fsType: string;
  size: number;
  usedBy?: string;
}

export interface PluginLogsResponse {
  lines: PluginLogLine[];
}

/** One line a plugin wrote. `time` is when (RFC 3339, UTC), left out when it isn't known. */
export interface PluginLogLine {
  time?: string;
  text: string;
}

/**
 * An extra wired port for devices (a USB Ethernet adapter). A device plugged into it joins the
 * hotspot, after the same safety check as the Ethernet client port.
 */
export interface LANPortStatus {
  /** The interface, e.g. "enx00e04c680001". */
  name: string;
  cablePlugged: boolean;
  /** True when the port has joined the hotspot network. */
  connected: boolean;
  /** Set when a DHCP server answered on the port, so it was kept off the hotspot network. */
  foreignDhcpServer?: string;
}

/** Where the welcome page is shown. */
export type WelcomeNetworks = 'hotspot' | 'guest' | 'both';

/**
 * GET/PUT /api/v1/settings/welcome: the welcome page, the owner's own captive portal. Until a
 * device on `networks` accepts it, its web requests show the page and it gets no internet.
 */
export interface WelcomeSettings {
  enabled: boolean;
  networks: WelcomeNetworks;
  /** 1 to 60 characters. */
  title: string;
  /** Up to 1,000 characters; "" for none. */
  message: string;
  /** House rules or terms (up to 10,000 characters); with them the page has an "I agree" box. */
  terms: string;
  /** 4 to 32 characters; "" for no code. */
  accessCode: string;
  /** How long an acceptance lasts; 0 until the owner asks again on the Clients page. */
  askAgainHours: number;
  /** Also show it to devices on wired ports (otherwise they are let through). */
  includeWired: boolean;
  /** The page's color, like "#0c8a9c". */
  color: string;
  /** A PNG or JPEG data: URL (up to about 100 KB); "" for none. */
  logo: string;
  /** A page of the owner's own (HTML, up to 64 KB) shown instead; "" for the built-in one. */
  customPage: string;
  /** Read-only: how many devices may use the internet now. */
  accepted: number;
}

/** A phone sharing its connection over USB, or a USB modem that works like one (HiLink). */
export interface PhoneUplinkStatus {
  interface: string;
  kind: 'iphone' | 'android' | 'modem';
  /** What the device calls itself over USB ("Pixel 8"), or "". */
  name?: string;
  /** True when it gave RasBeacon an address. */
  connected: boolean;
  /** Data through it since it was plugged in. */
  receivedBytes: number;
  sentBytes: number;
}

/** What a mobile broadband modem is doing. */
export type ModemState =
  | 'connected'
  | 'connecting'
  | 'searching'
  | 'pin-needed'
  | 'pin-refused'
  | 'puk-needed'
  | 'no-sim'
  | 'starting'
  | 'failed';

/** A mobile broadband modem (one that needs ModemManager: QMI or MBIM). */
export interface ModemStatus {
  /** "Quectel EM12-G", or "Mobile modem". */
  name: string;
  state: ModemState;
  operator?: string;
  /** "5G", "4G" or "3G". */
  network?: string;
  signalPercent: number;
  /** PIN attempts the SIM has left; -1 when unknown. */
  pinTries: number;
  /** The last problem connecting, in ModemManager's words. */
  error?: string;
}

/**
 * GET/PUT /api/v1/settings/modem. `password` and `pin` are write-only: empty keeps the saved
 * one; `forgetPassword`/`forgetPin` remove it.
 */
export interface ModemSettings {
  apn: string;
  username: string;
  password?: string;
  hasPassword: boolean;
  pin?: string;
  hasPin: boolean;
  forgetPassword?: boolean;
  forgetPin?: boolean;
}
