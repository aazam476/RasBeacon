import type {
  AccessSettings,
  Client,
  DNSSettings,
  PortForwardRule,
  GuestSettings,
  HotspotSettings,
  HTTPSSettings,
  NearbyNetwork,
  PluginKeptData,
  PluginsState,
  SetupState,
  SSHSettings,
  Status,
  SwapSettings,
  UpdateSettings,
  UplinkSettings,
  UsageSettings,
  VPNSettings,
} from '../types';
import { createMockPlugins } from './plugins';

/**
 * One consistent sample router for the demo, merged from the design mockups and the original
 * UI's gallery: every screen shows the same facts. Scenarios (scenarios.ts) change parts of it.
 */
export interface MockRouter {
  setup: SetupState;
  status: Status;
  hotspotSettings: HotspotSettings;
  guestSettings: GuestSettings;
  uplinkSettings: UplinkSettings;
  vpnSettings: VPNSettings;
  sshSettings: SSHSettings;
  dnsSettings: DNSSettings;
  /** The port forwarding rules as saved (portForwarding.ts fills in the rest of the page). */
  portForwardingRules: PortForwardRule[];
  swapSettings: SwapSettings;
  updateSettings: UpdateSettings;
  accessSettings: AccessSettings;
  usageSettings: UsageSettings;
  plugins: PluginsState;
  /** Data kept when a plugin was removed, by plugin ID, that its next install asks about. */
  keptPluginData: Record<string, PluginKeptData>;
  httpsSettings: HTTPSSettings;
  nearbyNetworks: NearbyNetwork[];
  /** What "Show password" reveals. */
  hotspotPassword: string;
}

const clients: Client[] = [
  {
    hostname: 'iphone-15-pro-personal-icloud-private-relay.lan',
    mac: 'bc:d0:74:a1:8f:2c',
    ip: '192.168.8.102',
    signalDbm: -52,
    connectedSeconds: 15_120,
    band: '5GHz',
    downMbps: 32.4,
    upMbps: 2.1,
    downBytes: 1_840_000_000,
    upBytes: 212_000_000,
    internetAccess: true,
    isThisDevice: true,
  },
  {
    hostname: 'mbp-16-m2-max.lan',
    name: 'Work laptop',
    mac: 'f4:d4:88:62:3b:01',
    ip: '192.168.8.140',
    signalDbm: -48,
    connectedSeconds: 13_680,
    band: '5GHz',
    downMbps: 15.1,
    upMbps: 11.4,
    downBytes: 6_420_000_000,
    upBytes: 1_150_000_000,
    internetAccess: true,
  },
  {
    // On the Ethernet client port: no band, signal or measured traffic.
    hostname: 'desktop-office.lan',
    mac: 'a4:83:e7:12:34:56',
    ip: '192.168.8.131',
    fixedAddress: '192.168.8.131',
    signalDbm: 0,
    connectedSeconds: 7_260,
    band: '',
    downMbps: 0,
    upMbps: 0,
    downBytes: 0,
    upBytes: 0,
    internetAccess: true,
    wired: true,
  },
  {
    hostname: 'ipad-air-livingroom.lan',
    mac: '3c:06:30:19:9e:11',
    ip: '192.168.8.118',
    signalDbm: -64,
    connectedSeconds: 3_900,
    band: '2.4GHz',
    downMbps: 1.3,
    upMbps: 0.7,
    downBytes: 388_000_000,
    upBytes: 41_000_000,
    internetAccess: true,
  },
  {
    hostname: 'nintendo-switch-oled.lan',
    mac: '98:b6:e9:14:42:07',
    ip: '192.168.8.188',
    signalDbm: -59,
    connectedSeconds: 1_380,
    band: '5GHz',
    downMbps: 0,
    upMbps: 0,
    downBytes: 2_310_000_000,
    upBytes: 96_000_000,
    internetAccess: false,
    pausedSeconds: 1_320,
  },
  {
    hostname: 'kindle-paperwhite-wifi.lan',
    mac: '50:dc:e7:21:40:aa',
    ip: '192.168.8.205',
    signalDbm: -72,
    connectedSeconds: 29_700,
    band: '2.4GHz',
    downMbps: 0,
    upMbps: 0,
    downBytes: 12_400_000,
    upBytes: 3_100_000,
    internetAccess: true,
  },
];

const guestClients: Client[] = [
  {
    hostname: 'visitor-pixel-8.lan',
    mac: 'c2:aa:00:11:22:33',
    ip: '192.168.9.20',
    signalDbm: -60,
    connectedSeconds: 900,
    band: '5GHz',
    downMbps: 3.2,
    upMbps: 0.4,
    downBytes: 146_000_000,
    upBytes: 18_000_000,
    internetAccess: true,
  },
];

/** A fresh copy each time, so a scenario can change it freely. */
export function createMockRouter(): MockRouter {
  return {
    setup: {
      mode: 'normal',
      claimed: true,
      setupNetworkSecured: false,
      deviceShortId: '7162',
      lanAddress: '192.168.8.1',
      suggestedHotspotSsid: 'RasBeacon-7162',
      suggestedHotspotPassword: 'kq7m-2xvd-h8pt-rn4w',
      setupClosesInSeconds: 1_620,
    },

    status: {
      version: 'v1.0.0',
      boardModel: 'Raspberry Pi Compute Module 4 Rev 1.1',
      deviceId: 'rb-7f3a9c2e4b1d7162',
      uptimeSeconds: 1_233_720,
      mode: 'normal',
      plugins: [],
      hotspot: {
        running: true,
        ssid: 'RasBeacon-7162',
        band: '5GHz',
        channel: 44,
        security: 'wpa2-wpa3',
        interface: 'wlan1',
        lanAddress: '192.168.8.1',
        metered: false,
      },
      uplink: {
        connected: true,
        kind: 'wifi',
        interface: 'wlan0',
        address: '10.15.82.114',
        gateway: '10.15.82.1',
        ipv6: false,
        subnetMask: '255.255.240.0',
        interfaceMac: 'b8:27:eb:d4:11:80',
        wifi: {
          available: true,
          interface: 'wlan0',
          state: 'connected',
          ssid: 'GrandHyatt_Guest_5G',
          band: '5GHz',
          signalDbm: -54,
          bssid: 'b4:86:55:9a:1c:40',
          channel: 36,
          frequencyMhz: 5180,
        },
        ethernet: {
          present: true,
          role: 'uplink',
          cablePlugged: false,
          linkSpeed: '1 Gbps',
          duplex: '1000 Mbps Full Duplex',
        },
      },
      vpn: {
        enabled: true,
        killSwitch: true,
        state: 'up',
        handshakeAgeSeconds: 14,
        endpoint: '198.51.100.44:51820',
        receivedBytes: 148_600_000,
        sentBytes: 42_100_000,
      },
      portal: {
        detected: false,
        canStart: false,
        loginMode: false,
        secondsLeft: 0,
        canBrowse: false,
        browsing: false,
      },
      update: {
        board: 'rpi64',
        canInstall: true,
        installing: false,
        canCheckOnline: true,
        checkHours: 0,
      },
      radios: [
        {
          interface: 'wlan0',
          phy: 'phy0',
          driver: 'brcmfmac',
          isUsb: false,
          usbId: '',
          supportsAp: true,
          maxAps: 1,
          role: 'uplink',
        },
        {
          interface: 'wlan1',
          phy: 'phy1',
          driver: 'mt76x2u',
          isUsb: true,
          usbId: '0e8d:7612',
          supportsAp: true,
          maxAps: 2,
          role: 'hotspot',
        },
      ],
      clients,
      guest: { enabled: true, running: true, ssid: 'RasBeacon-Guests', clients: guestClients },
      warnings: [],

      throughput: {
        downMbps: 48.8,
        downPeakMbps: 62.4,
        upMbps: 14.2,
        upPeakMbps: 26.3,
        peakMbps: 84.1,
        sessionBytes: 3_400_000_000,
      },
      system: {
        cpuPercent: 24,
        cpuModel: 'Quad-Core ARM',
        ramUsedMb: 382,
        ramTotalMb: 1024,
        swapUsedMb: 12,
        swapTotalMb: 512,
        temperatureC: 42,
        thermalState: 'Nominal',
        cooling: 'Passive Alum',
        sensorStatus: 'Sensors OK',
        throttling: 'None',
      },
      device: {
        hostname: 'rasbeacon.lan',
        modelSpec: 'CM4-1GB-Lite',
        kernel: 'Linux 6.1.21-v8+ aarch64',
        kernelRelease: '6.1.21-v8+',
        appliance: 'Raspberry Pi CM4 Appliance (eMMC Boot)',
        bootDevice: 'eMMC Internal',
      },
      storage: {
        totalGb: 10,
        firmwareGb: 3.8,
        userGb: 2.4,
        medium: 'eMMC',
        filesystem: 'SquashFS + OverlayFS',
        health: '99%',
      },
      lan: { maxClients: 16, subnet: '192.168.8.0/24', dhcpRange: '192.168.8.100 - 192.168.8.250' },
      speedtest: {
        provider: 'Cloudflare',
        running: false,
        last: {
          server: 'San Jose, CA (SJC)',
          ageSeconds: 120,
          downloadMbps: 942,
          uploadMbps: 885,
          pingMs: 4,
        },
      },
      lanPorts: [],
      ethernetDiagnostics: null,
    },

    hotspotSettings: {
      ssid: 'RasBeacon-7162',
      band: '5GHz',
      channel: 44,
      security: 'wpa2-wpa3',
      country: 'US',
      metered: false,
    },
    hotspotPassword: 'kq7m-2xvd-h8pt-rn4w',

    guestSettings: { enabled: true, ssid: 'RasBeacon-Guests', hasPassword: true, supported: true },

    uplinkSettings: {
      ethernetRole: 'uplink',
      ethernetPresent: true,
      wifiUplinkAvailable: true,
      networks: [
        { id: 'n1', ssid: 'GrandHyatt_Guest_5G', security: 'open', hidden: false, hasPassword: false },
        {
          id: 'n2',
          ssid: 'eduroam',
          security: 'enterprise',
          hidden: false,
          hasPassword: true,
          enterprise: {
            method: 'ttls-pap',
            identity: 'user@example.edu',
            anonymousIdentity: 'anonymous@example.edu',
            serverName: 'radius.example.edu',
            allowNoCertificateCheck: false,
            caCertSummary: 'USERTrust RSA Certification Authority (valid until 2038-01-18)',
            clientCertSummary: '',
            hasClientKey: false,
          },
        },
        { id: 'n3', ssid: 'Starbucks_WiFi_HighSpeed', security: 'personal', hidden: false, hasPassword: true },
        { id: 'n4', ssid: 'Home-Office', security: 'personal', hidden: true, hasPassword: true },
      ],
    },

    nearbyNetworks: [
      { ssid: 'GrandHyatt_Guest_5G', signalDbm: -54, frequencyMhz: 5180, security: 'open', label: 'Open' },
      {
        ssid: 'GrandHyatt_Staff_Secure',
        signalDbm: -68,
        frequencyMhz: 5200,
        security: 'enterprise',
        label: 'WPA3-Enterprise',
      },
      {
        ssid: 'Starbucks_WiFi_HighSpeed',
        signalDbm: -72,
        frequencyMhz: 2437,
        security: 'personal',
        label: 'WPA2-Personal',
      },
      {
        ssid: 'MobileMiFi_Roaming_7A',
        signalDbm: -81,
        frequencyMhz: 2412,
        security: 'personal',
        label: 'WPA2-Personal',
      },
      { ssid: 'Lobby_Display_Bridge', signalDbm: -88, frequencyMhz: 2462, security: 'other', label: 'WEP' },
    ],

    vpnSettings: {
      enabled: true,
      killSwitch: true,
      activeProfile: 'hotel-tunnel-01.conf',
      profiles: [
        {
          name: 'hotel-tunnel-01.conf',
          addresses: ['10.66.0.2/32'],
          dns: ['10.66.0.1'],
          mtu: 0,
          peerPublicKey: 'xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=',
          endpoint: '198.51.100.44:51820',
          allowedIps: ['0.0.0.0/0', '::/0'],
          persistentKeepalive: 25,
          publicKey: 'HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=',
          hasPresharedKey: true,
          routedIpv6Prefix: '',
        },
        {
          name: 'work-vpn-chicago.conf',
          addresses: ['10.20.4.17/32'],
          dns: ['10.20.0.53'],
          mtu: 1420,
          peerPublicKey: 'TrMvSoP4jYQlY6RIzBgbssQqY3vxI2Pi+y71lOWWXX0=',
          endpoint: 'vpn.chicago.example.com:51820',
          allowedIps: ['0.0.0.0/0', '::/0'],
          persistentKeepalive: 0,
          publicKey: 'oRDdGkzLzrJ4yVdS7jNi5yN0d4Wg8E8fO2Zy7S6ezQ8=',
          hasPresharedKey: false,
          routedIpv6Prefix: '',
        },
        {
          name: 'home-gateway.conf',
          addresses: ['10.10.0.2/32'],
          dns: ['10.10.0.1'],
          mtu: 0,
          peerPublicKey: 'Fi1o7tXv2BhLqv7yy2t6FgZw5ZqOuT0zR3l3w8mS2kM=',
          endpoint: 'home.example.net:51820',
          allowedIps: ['0.0.0.0/0', '::/0'],
          persistentKeepalive: 25,
          publicKey: 'yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=',
          hasPresharedKey: false,
          routedIpv6Prefix: '2001:db8:1234:5678::/64',
        },
      ],
    },

    dnsSettings: { servers: [], withVpn: false },
    portForwardingRules: [
      {
        id: 'pf-minecraft',
        name: 'Minecraft server',
        protocol: 'tcp',
        outsidePort: 25565,
        mac: 'a4:83:e7:12:34:56',
        enabled: true,
      },
      {
        id: 'pf-steam',
        name: 'Steam games',
        protocol: 'udp',
        outsidePort: 27015,
        outsidePortEnd: 27030,
        mac: 'a4:83:e7:12:34:56',
        enabled: false,
      },
    ],
    swapSettings: { sizeGb: 2, maxGb: 26 },
    updateSettings: { checkHours: 0, notifyUrl: '' },
    accessSettings: { allowListOn: false, allowed: [], maxDevices: 0 },
    plugins: createMockPlugins(),
    keptPluginData: {},
    usageSettings: { limitMb: 0, since: '2026-10-01T08:00:00Z', devicesPaused: 0 },

    sshSettings: {
      enabled: true,
      authorizedKeys: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK0wmN/Cr3JXqmLW7u+g9pTh+wyqDHpSQEIQczXkVx9q owner@laptop\n',
    },

    httpsSettings: {
      enabled: true,
      uploaded: false,
      subject: 'CN=RasBeacon 7162',
      fingerprint: '3F:9A:12:C4:7E:01:55:B2:9D:4C:10:E8:77:AB:02:6C:91:3E:D5:48:0F:22:A7:6B:C3:19:E0:84:5D:2A:F1:66',
      validUntil: '2036-10-01',
    },
  };
}
