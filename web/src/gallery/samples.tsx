// Sample screens for the design gallery (gallery.html): every view in its
// important states, filled with fixed example data instead of a real
// device. Nothing here talks to the API or runs any rules; the buttons do
// nothing. It exists so a designer can see and restyle every screen
// without a RasBeacon (or a simulated one).
//
// Each sample is a view with hand-written props. When a view's props
// change, TypeScript points at the samples that need updating.

import type { ReactElement } from "react";
import type {
  Client,
  GuestSettings,
  HTTPSSettings,
  PortalStatus,
  Status,
  UplinkSettings,
  VPNSettings,
} from "../api/types";
import { LayoutView } from "../app/LayoutView";
import type { Page } from "../app/useApp";
import { NoticeView } from "../screens/Notice/NoticeView";
import { SetupView } from "../screens/Setup/SetupView";
import type { SetupLogic } from "../screens/Setup/useSetup";
import { LoginView } from "../screens/Login/LoginView";
import { StatusView } from "../screens/Status/StatusView";
import { UplinkSettingsView } from "../screens/Uplink/UplinkSettingsView";
import type { UplinkSettingsLogic } from "../screens/Uplink/useUplinkSettings";
import { EMPTY_NEW_NETWORK } from "../screens/Uplink/networkList";
import { VPNSettingsView } from "../screens/VPN/VPNSettingsView";
import type { VPNSettingsLogic } from "../screens/VPN/useVPNSettings";
import { toForm } from "../screens/VPN/vpnForm";
import { HotspotSettingsView } from "../screens/HotspotSettings/HotspotSettingsView";
import type { HotspotSettingsLogic } from "../screens/HotspotSettings/useHotspotSettings";
import { GuestSettingsView } from "../screens/Guest/GuestSettingsView";
import type { GuestSettingsLogic } from "../screens/Guest/useGuestSettings";
import { SSHSettingsView } from "../screens/SSHSettings/SSHSettingsView";
import { HTTPSSettingsView } from "../screens/HTTPS/HTTPSSettingsView";
import type { HTTPSSettingsLogic } from "../screens/HTTPS/useHTTPSSettings";
import { SystemView } from "../screens/System/SystemView";
import type { SystemLogic } from "../screens/System/useSystem";
import { PortalView } from "../screens/Portal/PortalView";
import type { PortalLogic } from "../screens/Portal/usePortal";
import type { PortalBrowserLogic } from "../screens/Portal/usePortalBrowser";

const noop = () => {};

// --- Example data ---

const clients: Client[] = [
  {
    mac: "3a:1f:22:aa:bb:cc",
    ip: "10.87.0.23",
    hostname: "Alis-iPhone",
    signalDbm: -48,
    connectedSeconds: 367,
  },
  {
    mac: "8e:00:11:22:33:44",
    ip: "10.87.0.24",
    hostname: "Pixel-8",
    signalDbm: -71,
    connectedSeconds: 42,
  },
];

const portalNone: PortalStatus = {
  detected: false,
  canStart: false,
  loginMode: false,
  secondsLeft: 0,
  canBrowse: false,
  browsing: false,
};

const status: Status = {
  version: "v1.0.0",
  boardModel: "Raspberry Pi 4 Model B Rev 1.5",
  deviceId: "rb-7f3a9c2e4b1d7162",
  uptimeSeconds: 3725,
  mode: "normal",
  hotspot: {
    running: true,
    ssid: "Ali's Travel Wi-Fi",
    band: "5GHz",
    channel: 36,
    security: "wpa2-wpa3",
    interface: "ap0",
    lanAddress: "10.87.0.1",
    metered: false,
  },
  uplink: {
    connected: true,
    kind: "wifi",
    interface: "wlan0",
    address: "192.168.4.20",
    gateway: "192.168.4.1",
    ipv6: false,
    wifi: {
      available: true,
      interface: "wlan0",
      state: "connected",
      ssid: "Corner Café",
      band: "2.4GHz",
      signalDbm: -58,
    },
    ethernet: { present: true, role: "uplink", cablePlugged: false },
  },
  vpn: {
    enabled: true,
    killSwitch: true,
    state: "up",
    handshakeAgeSeconds: 12,
    endpoint: "203.0.113.5:51820",
    receivedBytes: 48_000_000,
    sentBytes: 6_200_000,
  },
  portal: portalNone,
  update: {
    board: "pi4",
    canInstall: true,
    installing: false,
    canCheckOnline: true,
    checkOnline: false,
  },
  radios: [
    {
      interface: "wlan0",
      phy: "phy0",
      driver: "brcmfmac",
      isUsb: false,
      usbId: "",
      supportsAp: true,
      maxAps: 1,
      role: "uplink",
    },
    {
      interface: "ap0",
      phy: "phy1",
      driver: "mt76x2u",
      isUsb: true,
      usbId: "0e8d:7612",
      supportsAp: true,
      maxAps: 2,
      role: "hotspot",
    },
  ],
  clients,
  guest: {
    enabled: true,
    running: true,
    ssid: "Ali's Guests",
    clients: [
      {
        mac: "c2:aa:00:11:22:33",
        ip: "10.87.1.20",
        hostname: "Visitor-Phone",
        signalDbm: -60,
        connectedSeconds: 900,
      },
    ],
  },
  warnings: [],
};

const uplinkSettings: UplinkSettings = {
  ethernetRole: "uplink",
  ethernetPresent: true,
  wifiUplinkAvailable: true,
  networks: [
    { id: "a1", ssid: "Corner Café", security: "personal", hidden: false, hasPassword: true },
    {
      id: "b2",
      ssid: "eduroam",
      security: "enterprise",
      hidden: false,
      hasPassword: true,
      enterprise: {
        method: "ttls-pap",
        identity: "NetID@example.edu",
        anonymousIdentity: "anonymous@example.edu",
        serverName: "*.example.edu",
        allowNoCertificateCheck: false,
        caCertSummary: "Example University Root CA (valid until 2031-04-01)",
        clientCertSummary: "",
        hasClientKey: false,
      },
    },
    { id: "", ssid: "Hotel Guest", security: "open", hidden: false, hasPassword: false },
  ],
};

const vpnSaved: VPNSettings = {
  enabled: true,
  killSwitch: true,
  configured: true,
  addresses: ["10.66.0.2/32"],
  dns: ["10.66.0.1"],
  mtu: 0,
  peerPublicKey: "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=",
  endpoint: "vpn.example.com:51820",
  allowedIps: ["0.0.0.0/0", "::/0"],
  persistentKeepalive: 25,
  publicKey: "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=",
  hasPresharedKey: false,
  routedIpv6Prefix: "",
};

const httpsSettings: HTTPSSettings = {
  enabled: true,
  uploaded: false,
  subject: "CN=rasbeacon.lan",
  fingerprint:
    "3F:9A:12:C4:7E:01:55:B2:9D:4C:10:E8:77:AB:02:6C:91:3E:D5:48:0F:22:A7:6B:C3:19:E0:84:5D:2A:F1:66",
  validUntil: "2036-10-01",
};

const guestSettings: GuestSettings = {
  enabled: true,
  ssid: "Ali's Guests",
  hasPassword: true,
  supported: true,
};

/** A drawing that stands in for the hotel's login page picture. */
const hotelPicture =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="390" height="591">
<rect width="390" height="591" fill="#fff"/>
<text x="24" y="50" font-family="sans-serif" font-size="26" font-weight="bold">Grand Hotel Wi-Fi</text>
<text x="24" y="100" font-family="sans-serif" font-size="16">Room number</text>
<rect x="24" y="115" width="300" height="40" fill="#fff" stroke="#888"/>
<rect x="24" y="175" width="110" height="36" rx="4" fill="#eee" stroke="#888"/>
<text x="44" y="199" font-family="sans-serif" font-size="18">Connect</text>
</svg>`);

// --- Props helpers: one base per screen, varied per sample ---

const setupBase: SetupLogic = {
  values: {
    adminPassword: "",
    adminPasswordConfirm: "",
    hotspotSsid: "RasBeacon-7162",
    hotspotPassword: "kq7m-2xvd-h8pt-rn4w",
    hotspotBand: "2.4GHz",
  },
  setField: noop,
  errors: {},
  formError: null,
  submitting: false,
  result: null,
  submit: noop,
  setupNetworkSecured: false,
  minutesLeft: 27,
};

const uplinkBase: UplinkSettingsLogic = {
  values: uplinkSettings,
  loadError: null,
  setEthernetRole: noop,
  moveUp: noop,
  moveDown: noop,
  remove: noop,
  setPassword: noop,
  networkErrors: {},
  newNetwork: EMPTY_NEW_NETWORK,
  setNewNetworkField: noop,
  newNetworkErrors: {},
  setNewNetworkFiles: noop,
  applyEduroamPreset: noop,
  addNetwork: noop,
  scan: noop,
  scanning: false,
  nearby: null,
  scanError: null,
  pickNearby: noop,
  dirty: false,
  save: noop,
  saving: false,
  saved: false,
  saveError: null,
};

const vpnBase: VPNSettingsLogic = {
  saved: vpnSaved,
  loadError: null,
  importText: "",
  setImportText: noop,
  setImportFile: noop,
  importConf: noop,
  importing: false,
  importError: null,
  form: toForm(vpnSaved),
  setField: noop,
  save: noop,
  saving: false,
  saveError: null,
  errorField: null,
  generateKey: noop,
  message: null,
};

const hotspotBase: HotspotSettingsLogic = {
  values: {
    ssid: "Ali's Travel Wi-Fi",
    band: "5GHz",
    channel: 0,
    security: "wpa2-wpa3",
    country: "US",
    metered: false,
    password: "",
  },
  setField: noop,
  errors: {},
  loadError: null,
  saveError: null,
  saving: false,
  saved: false,
  save: noop,
  revealAdminPassword: "",
  setRevealAdminPassword: noop,
  revealedPassword: null,
  revealError: null,
  reveal: noop,
};

const guestBase: GuestSettingsLogic = {
  values: { ...guestSettings, password: "" },
  setField: noop,
  errors: {},
  loadError: null,
  saveError: null,
  saving: false,
  saved: false,
  save: noop,
};

const httpsBase: HTTPSSettingsLogic = {
  settings: httpsSettings,
  loadError: null,
  httpsAddress: "https://10.87.0.1/",
  onHTTPS: false,
  setEnabled: noop,
  generateNew: noop,
  certificateText: "",
  setCertificateText: noop,
  keyText: "",
  setKeyText: noop,
  setFile: noop,
  upload: noop,
  busy: false,
  error: null,
  message: null,
};

const systemBase: SystemLogic = {
  version: "v1.0.0",
  update: status.update,
  updateFile: null,
  setUpdateFile: noop,
  installUpdate: noop,
  installing: false,
  setCheckOnline: noop,
  diagnosticsUrl: "#",
  message: null,
  error: null,
  reboot: noop,
  resetPassword: "",
  setResetPassword: noop,
  factoryReset: noop,
  logout: noop,
};

const browserBase: PortalBrowserLogic = {
  frame: undefined,
  imageSrc: "",
  open: noop,
  close: noop,
  pointerDown: noop,
  pointerUp: noop,
  wheel: noop,
  text: "",
  setText: noop,
  sendText: noop,
  pressKey: noop,
  back: noop,
  reload: noop,
  busy: false,
  error: null,
};

const portalBase: PortalLogic = {
  status: portalNone,
  loadError: null,
  loginUrl: "http://login.grandhotel.example/",
  start: noop,
  extend: noop,
  stop: noop,
  check: noop,
  browser: browserBase,
  busy: false,
  actionError: null,
};

/** A main page inside the frame (navigation). */
function inFrame(page: Page, content: ReactElement): ReactElement {
  return (
    <LayoutView page={page} setPage={noop}>
      {content}
    </LayoutView>
  );
}

// --- The samples ---

export interface Sample {
  /** Group, e.g. "Status". */
  screen: string;
  /** Which state, e.g. "VPN down, kill switch on". */
  state: string;
  render: () => ReactElement;
}

const portalDetected: PortalStatus = {
  ...portalNone,
  detected: true,
  canBrowse: true,
  canStart: true,
  loginUrl: "http://login.grandhotel.example/",
};

export const SAMPLES: Sample[] = [
  // Full-page notices
  { screen: "Notice", state: "Loading", render: () => <NoticeView kind="loading" /> },
  {
    screen: "Notice",
    state: "Can't reach RasBeacon",
    render: () => <NoticeView kind="unreachable" detail="Network error" retry={noop} />,
  },
  {
    screen: "Notice",
    state: "Setup window closed",
    render: () => <NoticeView kind="setup-closed" retry={noop} />,
  },
  {
    screen: "Notice",
    state: "setup.txt problem",
    render: () => <NoticeView kind="setup-error" retry={noop} />,
  },
  {
    screen: "Notice",
    state: "No Wi-Fi radio",
    render: () => <NoticeView kind="no-radio" retry={noop} />,
  },

  // Setup wizard
  {
    screen: "Setup",
    state: "Empty, open setup network",
    render: () => <SetupView {...setupBase} />,
  },
  {
    screen: "Setup",
    state: "Field errors",
    render: () => (
      <SetupView
        {...setupBase}
        values={{ ...setupBase.values, adminPassword: "short", adminPasswordConfirm: "different" }}
        errors={{
          adminPassword: "Use at least 10 characters.",
          adminPasswordConfirm: "The two passwords don't match.",
        }}
        setupNetworkSecured
      />
    ),
  },
  { screen: "Setup", state: "Submitting", render: () => <SetupView {...setupBase} submitting /> },
  {
    screen: "Setup",
    state: "Done",
    render: () => (
      <SetupView
        {...setupBase}
        result={{
          hotspotSsid: "Ali's Travel Wi-Fi",
          switchInSeconds: 5,
          adminUrl: "http://10.87.0.1/",
        }}
      />
    ),
  },

  // Login
  {
    screen: "Login",
    state: "Empty",
    render: () => (
      <LoginView password="" setPassword={noop} error={null} submitting={false} submit={noop} />
    ),
  },
  {
    screen: "Login",
    state: "Wrong password",
    render: () => (
      <LoginView
        password="wrong"
        setPassword={noop}
        error="Wrong password."
        submitting={false}
        submit={noop}
      />
    ),
  },

  // Status
  {
    screen: "Status",
    state: "All good (VPN up, guests on)",
    render: () =>
      inFrame("status", <StatusView status={status} error={null} loading={false} reload={noop} />),
  },
  {
    screen: "Status",
    state: "Loading",
    render: () =>
      inFrame("status", <StatusView status={undefined} error={null} loading reload={noop} />),
  },
  {
    screen: "Status",
    state: "VPN down, kill switch on (no internet)",
    render: () =>
      inFrame(
        "status",
        <StatusView
          status={{
            ...status,
            vpn: { ...status.vpn, state: "down", handshakeAgeSeconds: 600 },
            warnings: [
              "The VPN tunnel is not working, and the kill switch is on: your devices have no internet until it works again.",
            ],
          }}
          error={null}
          loading={false}
          reload={noop}
        />,
      ),
  },
  {
    screen: "Status",
    state: "VPN broken, kill switch off (WITHOUT the VPN)",
    render: () =>
      inFrame(
        "status",
        <StatusView
          status={{
            ...status,
            vpn: { ...status.vpn, killSwitch: false, state: "direct", handshakeAgeSeconds: 900 },
            warnings: [
              "The VPN tunnel is not working, and the kill switch is off: your devices are using the internet WITHOUT the VPN.",
            ],
          }}
          error={null}
          loading={false}
          reload={noop}
        />,
      ),
  },
  {
    screen: "Status",
    state: "Hotel login page detected",
    render: () =>
      inFrame(
        "status",
        <StatusView
          status={{
            ...status,
            vpn: { ...status.vpn, state: "connecting", handshakeAgeSeconds: -1 },
            portal: portalDetected,
          }}
          error={null}
          loading={false}
          reload={noop}
        />,
      ),
  },
  {
    screen: "Status",
    state: "Not connected, no devices, warnings",
    render: () =>
      inFrame(
        "status",
        <StatusView
          status={{
            ...status,
            uplink: {
              ...status.uplink,
              connected: false,
              kind: "",
              address: "",
              wifi: { ...status.uplink.wifi, state: "searching" },
            },
            vpn: { ...status.vpn, enabled: false, state: "off" },
            clients: [],
            guest: { enabled: false, running: false, ssid: "", clients: [] },
            hotspot: { ...status.hotspot, metered: true },
            warnings: [
              "No USB Wi-Fi adapter found, so the built-in Wi-Fi is running the hotspot. It can only hold about 8 devices and cannot also join an upstream network. Plug in a supported USB adapter and reboot.",
            ],
          }}
          error="Can't reach RasBeacon. Check that this device is connected to it."
          loading={false}
          reload={noop}
        />,
      ),
  },

  // Internet
  {
    screen: "Internet",
    state: "Saved networks (one unsaved)",
    render: () => inFrame("internet", <UplinkSettingsView {...uplinkBase} dirty />),
  },
  {
    screen: "Internet",
    state: "Scan results",
    render: () =>
      inFrame(
        "internet",
        <UplinkSettingsView
          {...uplinkBase}
          nearby={[
            {
              ssid: "Corner Café",
              signalDbm: -58,
              frequencyMhz: 2437,
              security: "personal",
              label: "WPA2-Personal",
            },
            {
              ssid: "Hotel Guest",
              signalDbm: -63,
              frequencyMhz: 5180,
              security: "open",
              label: "Open",
            },
            {
              ssid: "eduroam",
              signalDbm: -71,
              frequencyMhz: 5500,
              security: "enterprise",
              label: "WPA2-Enterprise",
            },
            {
              ssid: "Neighbour 5G",
              signalDbm: -80,
              frequencyMhz: 2462,
              security: "personal",
              label: "WPA3-Personal",
            },
          ]}
        />,
      ),
  },
  {
    screen: "Internet",
    state: "Scanning",
    render: () => inFrame("internet", <UplinkSettingsView {...uplinkBase} scanning />),
  },
  {
    screen: "Internet",
    state: "Adding eduroam (Enterprise form, errors)",
    render: () =>
      inFrame(
        "internet",
        <UplinkSettingsView
          {...uplinkBase}
          newNetwork={{
            ...EMPTY_NEW_NETWORK,
            ssid: "eduroam",
            security: "enterprise",
            identity: "NetID@example.edu",
            anonymousIdentity: "anonymous@example.edu",
          }}
          newNetworkErrors={{
            caCert: "Choose the network's CA certificate file.",
            serverName: "Enter the login server's name.",
          }}
        />,
      ),
  },
  {
    screen: "Internet",
    state: "Adding with EAP-TLS (certificate files)",
    render: () =>
      inFrame(
        "internet",
        <UplinkSettingsView
          {...uplinkBase}
          newNetwork={{
            ...EMPTY_NEW_NETWORK,
            ssid: "CorpWiFi",
            security: "enterprise",
            method: "tls",
            identity: "ali@corp.example",
            caCert: { name: "corp-root.pem", content: "" },
          }}
        />,
      ),
  },
  {
    screen: "Internet",
    state: "Saved; no Wi-Fi radio free; server error",
    render: () =>
      inFrame(
        "internet",
        <UplinkSettingsView
          {...uplinkBase}
          values={{ ...uplinkSettings, wifiUplinkAvailable: false }}
          networkErrors={{ 0: "Must be 8 to 63 characters long." }}
          saved
        />,
      ),
  },

  // VPN
  {
    screen: "VPN",
    state: "Not set up yet",
    render: () =>
      inFrame(
        "vpn",
        <VPNSettingsView
          {...vpnBase}
          saved={{
            ...vpnSaved,
            configured: false,
            enabled: false,
            endpoint: "",
            peerPublicKey: "",
            addresses: [],
            dns: [],
            allowedIps: [],
            publicKey: "",
          }}
          form={toForm({
            ...vpnSaved,
            configured: false,
            enabled: false,
            endpoint: "",
            peerPublicKey: "",
            addresses: [],
            dns: [],
            allowedIps: [],
          })}
        />,
      ),
  },
  {
    screen: "VPN",
    state: "Set up and on",
    render: () => inFrame("vpn", <VPNSettingsView {...vpnBase} message="Saved." />),
  },
  {
    screen: "VPN",
    state: "Import refused",
    render: () =>
      inFrame(
        "vpn",
        <VPNSettingsView
          {...vpnBase}
          importText={"[Interface]\nPostUp = iptables ...\n"}
          importError="Line 2: RasBeacon never runs commands from WireGuard files (PostUp). Remove that line."
        />,
      ),
  },

  // Hotspot
  {
    screen: "Hotspot",
    state: "Normal",
    render: () => inFrame("hotspot", <HotspotSettingsView {...hotspotBase} />),
  },
  {
    screen: "Hotspot",
    state: "Errors, password shown, metered on",
    render: () =>
      inFrame(
        "hotspot",
        <HotspotSettingsView
          {...hotspotBase}
          values={{ ...hotspotBase.values!, metered: true, password: "short" }}
          errors={{ password: "Must be 8 to 63 characters long." }}
          revealedPassword="kq7m-2xvd-h8pt-rn4w"
        />,
      ),
  },
  {
    screen: "Hotspot",
    state: "Saved",
    render: () => inFrame("hotspot", <HotspotSettingsView {...hotspotBase} saved />),
  },

  // Guests
  {
    screen: "Guests",
    state: "On",
    render: () => inFrame("guest", <GuestSettingsView {...guestBase} saved />),
  },
  {
    screen: "Guests",
    state: "First time (password needed)",
    render: () =>
      inFrame(
        "guest",
        <GuestSettingsView
          {...guestBase}
          values={{ ...guestSettings, enabled: true, hasPassword: false, password: "" }}
          errors={{ password: "Choose a password for the guest network." }}
        />,
      ),
  },
  {
    screen: "Guests",
    state: "Adapter can't run it",
    render: () =>
      inFrame(
        "guest",
        <GuestSettingsView
          {...guestBase}
          values={{ ...guestSettings, enabled: false, supported: false }}
        />,
      ),
  },

  // SSH
  {
    screen: "SSH",
    state: "Off",
    render: () =>
      inFrame(
        "ssh",
        <SSHSettingsView
          values={{ enabled: false, authorizedKeys: "" }}
          setEnabled={noop}
          setAuthorizedKeys={noop}
          loadError={null}
          saveError="Add at least one public key before turning SSH on."
          saving={false}
          saved={false}
          save={noop}
        />,
      ),
  },

  // HTTPS
  {
    screen: "HTTPS",
    state: "On",
    render: () => inFrame("https", <HTTPSSettingsView {...httpsBase} message="HTTPS is on." />),
  },
  {
    screen: "HTTPS",
    state: "Off",
    render: () =>
      inFrame(
        "https",
        <HTTPSSettingsView {...httpsBase} settings={{ ...httpsSettings, enabled: false }} />,
      ),
  },

  // System
  {
    screen: "System",
    state: "Normal",
    render: () => inFrame("system", <SystemView {...systemBase} />),
  },
  {
    screen: "System",
    state: "Update available, installing",
    render: () =>
      inFrame(
        "system",
        <SystemView
          {...systemBase}
          update={{
            ...status.update,
            checkOnline: true,
            latestVersion: "v1.1.0",
            latestUrl: "#",
            installing: true,
          }}
          installing
          message="Installing the update… RasBeacon restarts when it's done."
        />,
      ),
  },
  {
    screen: "System",
    state: "Update failed, no signing key",
    render: () =>
      inFrame(
        "system",
        <SystemView
          {...systemBase}
          update={{
            ...status.update,
            canInstall: false,
            message: "This file is not signed by a key this RasBeacon trusts.",
          }}
          error="Wrong admin password."
        />,
      ),
  },

  // Login-page notice (#portal): full page, no frame, opens in the phone's login sheet
  {
    screen: "Login-page notice",
    state: "Nothing to do",
    render: () => <PortalView {...portalBase} />,
  },
  {
    screen: "Login-page notice",
    state: "Both ways offered",
    render: () => <PortalView {...portalBase} status={portalDetected} />,
  },
  {
    screen: "Login-page notice",
    state: "Only the zero-leak login",
    render: () => <PortalView {...portalBase} status={{ ...portalDetected, canStart: false }} />,
  },
  {
    screen: "Login-page notice",
    state: "Opening RasBeacon's browser",
    render: () => (
      <PortalView
        {...portalBase}
        status={portalDetected}
        browser={{ ...browserBase, busy: true }}
      />
    ),
  },
  {
    screen: "Login-page notice",
    state: "Browser open, picture loading",
    render: () => <PortalView {...portalBase} status={{ ...portalDetected, browsing: true }} />,
  },
  {
    screen: "Login-page notice",
    state: "Browser open, login page shown",
    render: () => (
      <PortalView
        {...portalBase}
        status={{ ...portalDetected, browsing: true }}
        browser={{
          ...browserBase,
          imageSrc: hotelPicture,
          text: "412",
          frame: {
            running: true,
            seq: 7,
            image: "",
            width: 390,
            height: 591,
            url: "http://login.grandhotel.example/",
            title: "Grand Hotel Wi-Fi",
          },
        }}
      />
    ),
  },
  {
    screen: "Login-page notice",
    state: "Browser error",
    render: () => (
      <PortalView
        {...portalBase}
        status={{ ...portalDetected, browsing: true }}
        browser={{ ...browserBase, error: "The browser didn't respond: context deadline exceeded" }}
      />
    ),
  },
  {
    screen: "Login-page notice",
    state: "Login mode on (countdown)",
    render: () => (
      <PortalView
        {...portalBase}
        status={{ ...portalDetected, loginMode: true, secondsLeft: 512 }}
      />
    ),
  },
  {
    screen: "Login-page notice",
    state: "Couldn't load",
    render: () => (
      <PortalView
        {...portalBase}
        status={undefined}
        loadError="Can't reach RasBeacon. Check that this device is connected to it."
      />
    ),
  },
];
