import { createContext, useContext } from 'react';
import type {
  ClaimResponse,
  DNSSettings,
  UsageSettings,
  WiFiQR,
  PluginCatalog,
  PluginChoices,
  PluginDrive,
  PluginPreview,
  PluginsState,
  EthernetRole,
  GuestSettings,
  HotspotSettings,
  HTTPSSettings,
  NearbyNetwork,
  NewNetworkForm,
  PictureBox,
  PointerSpot,
  PortalBrowserFrame,
  PortalKey,
  PortalStatus,
  SavedNetwork,
  SetupForm,
  SetupState,
  SSHSettings,
  SwapSettings,
  Status,
  UplinkSettings,
  VPNDetailsForm,
  VPNSettings,
} from './types';

/** Data that arrives from the router: undefined until the first answer. */
export interface Loadable<T> {
  data: T | undefined;
  /** The last request's error message, or null. It can come together with older `data`. */
  error: string | null;
}

/**
 * What the app shows. Only `ready` shows the pages; the others take the whole screen.
 * The login page screen (`/#/portal`) also opens in the `login` phase, without logging in.
 */
export type AppPhase =
  /** The setup state and the session are loading. */
  | { kind: 'loading' }
  /** They couldn't be loaded. */
  | { kind: 'unreachable'; error: string }
  /** An unclaimed router that can't be set up (SetupState.mode). */
  | { kind: 'notice'; notice: 'setup-closed' | 'setup-error' | 'no-radio' }
  /** First boot: the setup wizard. */
  | { kind: 'setup' }
  | { kind: 'login' }
  | { kind: 'ready' };

/** Data a screen can ask to load again. */
export type DataKey =
  | 'status'
  | 'hotspotSettings'
  | 'guestSettings'
  | 'uplinkSettings'
  | 'vpnSettings'
  | 'sshSettings'
  | 'dnsSettings'
  | 'swapSettings'
  | 'usageSettings'
  | 'plugins'
  | 'httpsSettings'
  | 'portal';

/** RasBeacon's own browser, open on the hotel's login page (the zero-leak login). */
export interface PortalBrowserState {
  /** The latest picture's details, or undefined until the first one arrives. */
  frame: PortalBrowserFrame | undefined;
  /** The picture for an <img>, e.g. `data:image/jpeg;base64,…`; "" until the first one. */
  imageSrc: string;
  /** The browser's last problem, or null. */
  error: string | null;
}

/** Everything the screens can read about the router. */
export interface DeviceState {
  phase: AppPhase;
  /** GET /api/v1/setup. Undefined until it has loaded. */
  setup: SetupState | undefined;
  /** GET /api/v1/status. Status refreshes it every 10 seconds; other pages read it too. */
  status: Loadable<Status>;
  hotspotSettings: Loadable<HotspotSettings>;
  guestSettings: Loadable<GuestSettings>;
  uplinkSettings: Loadable<UplinkSettings>;
  vpnSettings: Loadable<VPNSettings>;
  sshSettings: Loadable<SSHSettings>;
  dnsSettings: Loadable<DNSSettings>;
  swapSettings: Loadable<SwapSettings>;
  usageSettings: Loadable<UsageSettings>;
  plugins: Loadable<PluginsState>;
  httpsSettings: Loadable<HTTPSSettings>;
  /** The last scan's results; null before the first scan. */
  nearbyNetworks: NearbyNetwork[] | null;
  /** GET /api/v1/portal, for the login page screen. Refresh it every 5 seconds there. */
  portal: Loadable<PortalStatus>;
  portalBrowser: PortalBrowserState;
  /** Link target of "Download diagnostics" (a plain file download), e.g. "/api/v1/diagnostics". */
  diagnosticsUrl: string;
}

/**
 * Everything the screens can ask the router to do. Every button in the UI calls one of these,
 * so this list is the complete set the real provider implements.
 *
 * Each action resolves once the router has finished AND the state shows the result. To report
 * a failure, reject with an Error whose message is a short sentence for the user: forms and
 * dialogs show it in place, other controls in the error toast. Or reject with `FieldErrors`
 * (errors.ts) to put messages next to inputs, using only the keys listed below (a message under
 * any other key is shown nowhere). The loading actions (`retry`, `reload`, `refreshStatus`)
 * are the exception: they resolve either way, and a failure goes into the state (`phase`,
 * `error`), where the screens show it. README.md, "Connecting the real router", has the details.
 */
export interface DeviceActions {
  // --- App ---------------------------------------------------------------------------------

  /** "Try again" on the full-page notices: load the setup state and the session again. */
  retry: () => Promise<void>;
  /** Load one piece of data again (the "Try again" buttons of the loading errors). */
  reload: (what: DataKey) => Promise<void>;

  // --- Setup and session -------------------------------------------------------------------

  /**
   * Claim the router (POST /api/v1/setup/claim) and answer with what to join next. Leave the
   * phase at `setup`: the screen shows "Setup complete" from the answer until the phone has
   * moved to the new network.
   * Field keys: adminPassword, adminPasswordConfirm, hotspotSsid, hotspotPassword.
   */
  finishSetup: (form: SetupForm) => Promise<ClaimResponse>;
  /** Afterwards the phase is `ready`. `stayLoggedIn` keeps the browser logged in for 30 days. */
  logIn: (password: string, stayLoggedIn: boolean) => Promise<void>;
  /** Afterwards the phase is `login`. */
  logOut: () => Promise<void>;

  // --- The hotel login page (works without logging in) -------------------------------------

  /** "Check again": look for a login page right away (portal action "check"). */
  checkPortal: () => Promise<void>;
  /** Login mode (portal action "start"). Then go to the login page in this window after ~2 s. */
  startLoginMode: () => Promise<void>;
  /** "10 more minutes" (portal action "extend"). */
  extendLoginMode: () => Promise<void>;
  stopLoginMode: () => Promise<void>;
  /** Open the login page in RasBeacon's browser, sized for this screen. */
  openPortalBrowser: () => Promise<void>;
  closePortalBrowser: () => Promise<void>;
  /**
   * A finger or mouse went down / up on the picture. The provider decides whether that was a tap
   * or a swipe and where on the page it was (browserGestures.ts in the original UI).
   */
  portalPointerDown: (spot: PointerSpot) => void;
  portalPointerUp: (spot: PointerSpot, box: PictureBox) => void;
  /** Mouse wheel over the picture. */
  portalWheel: (spot: PointerSpot, box: PictureBox, deltaY: number) => void;
  /** Type text into the field that has focus on the page. Problems go to `portalBrowser.error`. */
  portalType: (text: string) => void;
  portalKey: (key: PortalKey) => void;
  portalBack: () => void;
  portalReload: () => void;

  // --- Status ------------------------------------------------------------------------------

  /** Load the status again (also used by Clients' Refresh button). */
  refreshStatus: () => Promise<void>;

  // --- Internet ----------------------------------------------------------------------------

  /** Save UplinkSettings.ethernetRole (with the saved networks as they are). */
  setEthernetRole: (role: EthernetRole) => Promise<void>;
  /** Fill `nearbyNetworks` (takes about 5 seconds). */
  scanNearbyNetworks: () => Promise<void>;
  /**
   * Check the "add a network" form and turn it into a list entry (with `id` "" and the chosen
   * files read). Nothing is saved yet: the entry joins the list and is saved with the list.
   * `list` is the list as currently edited, to refuse duplicates.
   * Field keys: ssid, password, identity, clientCert, clientKey, caCert, serverName.
   */
  prepareNetwork: (form: NewNetworkForm, list: SavedNetwork[]) => Promise<SavedNetwork>;
  /**
   * Save the whole list, most preferred first (UplinkSettings.networks).
   * Field keys: `networks.<index>` for an error about the network at that position.
   */
  saveWifiNetworks: (networks: SavedNetwork[]) => Promise<void>;
  /** Leave the network that carries the internet now (for 10 minutes). */
  disconnectUplink: () => Promise<void>;
  /** Ask the uplink's DHCP server for a new lease. */
  renewDhcpLease: () => Promise<void>;
  /** Check the Ethernet port again. */
  rescanEthernetPort: () => Promise<void>;
  /**
   * The DNS servers for devices: the text as typed, one per line or comma-separated; empty uses
   * the network's. `withVpn` uses them while the VPN is on too. Field key: servers.
   */
  saveDnsSettings: (servers: string, withVpn: boolean) => Promise<void>;
  /**
   * Run Cloudflare's speed test (about 20 seconds) and update
   * `status.speedtest`.
   */
  runSpeedtest: () => Promise<void>;

  // --- VPN ---------------------------------------------------------------------------------

  setVpnEnabled: (enabled: boolean) => Promise<void>;
  setKillSwitch: (enabled: boolean) => Promise<void>;
  /** Switch to another profile. */
  setActiveVpnProfile: (name: string) => Promise<void>;
  /** Remove a profile and its keys. */
  removeVpnProfile: (name: string) => Promise<void>;
  /**
   * Import a WireGuard file as a new profile, from a chosen file or pasted text. `fileName` is
   * the chosen file's name, if any. Reject with the router's refusal for unsafe files.
   */
  importVpnProfile: (config: { text: string; fileName?: string }) => Promise<void>;
  /**
   * Save the server details of one profile.
   * Field keys: the VPNDetailsForm field names (endpoint, dns, mtu, …).
   */
  saveVpnDetails: (profile: string, form: VPNDetailsForm) => Promise<void>;
  /** Make a new key pair for one profile (the screen has already asked for confirmation). */
  generateVpnKey: (profile: string) => Promise<void>;

  // --- Hotspot and guests ------------------------------------------------------------------

  /** Field keys: ssid, password, channel, country. */
  saveHotspotSettings: (settings: HotspotSettings) => Promise<void>;
  /** Answer with the current Wi-Fi password. Reject when the admin password is wrong. */
  revealHotspotPassword: (adminPassword: string) => Promise<string>;
  /**
   * A QR code to join a network: the hotspot (needs the admin password; reject when it is wrong)
   * or the guest network.
   */
  wifiQr: (network: 'hotspot' | 'guest', adminPassword?: string) => Promise<WiFiQR>;
  /** Field keys: enabled, ssid, password. */
  saveGuestSettings: (settings: GuestSettings) => Promise<void>;

  // --- Clients -------------------------------------------------------------------------------------

  setClientInternetAccess: (mac: string, allowed: boolean) => Promise<void>;
  disconnectClient: (mac: string) => Promise<void>;
  flushClientLease: (mac: string) => Promise<void>;
  /** Name a device; "" goes back to the name it sends. Field key: name. */
  renameClient: (mac: string, name: string) => Promise<void>;
  /** The data limit per device on metered connections, in MB (0 = none). Field key: limitMb. */
  saveUsageLimit: (limitMb: number) => Promise<void>;
  /** Start counting data use again from zero (the screen has already asked). */
  resetUsage: () => Promise<void>;

  // --- SSH and HTTPS -----------------------------------------------------------------------

  /** Reject when SSH is turned on without a key. */
  saveSshSettings: (settings: SSHSettings) => Promise<void>;
  setHttpsEnabled: (enabled: boolean) => Promise<void>;
  /** The screen has already asked for confirmation. */
  makeNewCertificate: () => Promise<void>;
  /** Upload your own certificate and its private key (PEM files). */
  uploadCertificate: (files: { certificate: File; privateKey: File }) => Promise<void>;

  // --- System ------------------------------------------------------------------------------

  /** Upload and install a .rbupdate file; resolves when the router has accepted it. */
  installUpdate: (file: File) => Promise<void>;
  setCheckOnline: (enabled: boolean) => Promise<void>;
  /** The swap file's size in whole gigabytes (0 = off). Field key: sizeGb. */
  saveSwapSettings: (sizeGb: number) => Promise<void>;
  /** Resolves once the router answers again after restarting. */
  reboot: () => Promise<void>;
  /** Erase everything; afterwards the phase is `setup`. Reject when the admin password is wrong. */
  factoryReset: (adminPassword: string) => Promise<void>;
  /**
   * Make an encrypted backup and save it as a file. Field keys: adminPassword, backupPassword.
   */
  backUp: (adminPassword: string, backupPassword: string, includePlugins: boolean) => Promise<void>;
  /**
   * Restore a backup: RasBeacon restarts with its settings (log in with the backup's admin
   * password). Field keys: file, adminPassword, backupPassword.
   */
  restore: (file: File, adminPassword: string, backupPassword: string) => Promise<void>;

  // --- Plugins -----------------------------------------------------------------------------

  /** Check a .rba file and answer what installing it means. Field key: file. */
  previewPluginFile: (file: File) => Promise<PluginPreview>;
  /** The catalog; `refresh` asks GitHub again ("Check now"). */
  pluginCatalog: (refresh: boolean) => Promise<PluginCatalog>;
  /** Download a catalog plugin (through the VPN) for the install sheet. Field key: file. */
  previewCatalogPlugin: (source: string, file: string) => Promise<PluginPreview>;
  /** Download an installed plugin's update from the catalog for the install sheet. */
  previewPluginUpdate: (id: string) => Promise<PluginPreview>;
  /** Install a previewed plugin; it then shows as installing. Field keys: ports.<key>, settings.<key>, drive. */
  installPlugin: (stagingId: string, choices: PluginChoices) => Promise<void>;
  /** The install sheet was closed without installing. */
  cancelPluginInstall: (stagingId: string) => Promise<void>;
  /** Save a plugin's settings; it restarts if it runs. Field keys as installPlugin's. */
  savePluginSettings: (id: string, choices: PluginChoices) => Promise<void>;
  setPluginRunning: (id: string, running: boolean) => Promise<void>;
  removePlugin: (id: string, keepData: boolean) => Promise<void>;
  /** Its last 500 lines of output. */
  pluginLogs: (id: string) => Promise<string[]>;
  pluginDrives: () => Promise<PluginDrive[]>;
  /** The extra catalog sources ("owner/repo"). Field keys: sources.<n>. */
  savePluginSources: (sources: string[]) => Promise<void>;
}

export type Device = DeviceState & DeviceActions;

export const DeviceContext = createContext<Device | null>(null);

/** Read router data and call router actions from any screen. */
export function useDevice(): Device {
  const device = useContext(DeviceContext);
  if (!device) {
    throw new Error('useDevice() must be used inside a device provider, such as <DemoDeviceProvider>.');
  }
  return device;
}
