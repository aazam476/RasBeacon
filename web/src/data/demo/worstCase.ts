import type { Client } from '../types';
import type { DemoState } from './scenarios';

/**
 * The longest, oddest data the router can plausibly send, all at once, to check that every
 * screen holds up (wrapping, numbers, long lists). Limits used: SSIDs are at most 32 bytes of
 * UTF-8 (802.11); DHCP host names up to 63 characters; the hotspot takes 16 devices
 * (`lan.maxClients`). Values with no known limit get long but believable ones.
 */
export function applyWorstCase(demo: DemoState) {
  const { router } = demo;
  const { status } = router;

  // 32 bytes each: no spaces to wrap at, emoji, diacritics.
  const hotspotSsid = 'RasBeacon_Travel_Router_Office5G';
  const guestSsid = '🏖️ Gäste-WLAN Strandhaus';
  const uplinkSsid = 'AirportFreeWiFi_Terminal2_GateB';

  status.hotspot = { ...status.hotspot, ssid: hotspotSsid, metered: true, security: 'wpa3' };
  router.hotspotSettings = {
    ...router.hotspotSettings,
    ssid: hotspotSsid,
    country: 'DE',
    metered: true,
    security: 'wpa3',
  };
  // A long allow list.
  router.accessSettings = {
    allowListOn: true,
    allowed: Array.from({ length: 40 }, (_, i) => `3a:1f:22:${i.toString(16).padStart(2, '0')}:bb:cc`),
    maxDevices: 231,
  };
  status.uplink.wifi = { ...status.uplink.wifi, ssid: uplinkSsid, signalDbm: -91 };
  status.uplink.address = '100.127.254.253';
  status.uplink.gateway = '100.127.240.1';
  status.uplink.ipv6 = true;

  status.vpn = {
    ...status.vpn,
    endpoint: 'se-got-wg-001.relays.backup-servers.mullvad-vpn.example.net:51820',
    receivedBytes: 1_284_000_000_000,
    sentBytes: 98_700_000_000,
    handshakeAgeSeconds: 3_599,
    routedIpv6Prefix: '2001:db8:1234:5678::/64',
  };
  const longProfile = 'mullvad-se-got-wg-001-stockholm-gothenburg-backup-relay.conf';
  router.vpnSettings.activeProfile = longProfile;
  router.vpnSettings.profiles[0] = {
    ...router.vpnSettings.profiles[0],
    name: longProfile,
    endpoint: status.vpn.endpoint,
    routedIpv6Prefix: status.vpn.routedIpv6Prefix ?? '',
  };

  status.warnings = [
    'The built-in Wi-Fi is running the hotspot, so no radio is free to join Wi-Fi networks: plug in a USB Wi-Fi adapter for the hotspot, or use the Ethernet port.',
    'The CPU is at 87 °C and RasBeacon has slowed itself down to cool off. Give it some air, or take it out of its case for a while.',
    'The clock was wrong when RasBeacon started (1 January 1970), so certificates could not be checked until it was set from the network.',
    'Only 214 MB of storage is left. Old logs are being deleted to make room.',
    'The update check failed: GitHub answered "API rate limit exceeded for 203.0.113.7". RasBeacon tries again in 6 hours.',
  ];

  // 16 devices, the most the hotspot takes: long and short names, none at all, no address yet.
  const names = [
    'Christophers-MacBook-Pro-16-inch-2023-M3-Max-Space-Black.local',
    '',
    'J',
    'Đặng-Thị-Ngọc-Hân-iPhone',
    '王秀英的iPad',
    'android-8f3c2a1b9d7e6f50',
    'DESKTOP-7GH2K9Q',
    'Galaxy-Tab-S9-Ultra-Wohnzimmer',
    'roku-ultra-bedroom',
    'printer-hp-officejet-pro-9025e-2nd-floor-hallway',
    'ESP_3C61B4',
    'Samsung-Smart-Fridge-RF29BB8600QL-Kitchen',
    'thermostat',
    'Konstantin-Oberhauser-Wettsteins-ThinkPad-X1-Carbon',
  ];
  const clients: Client[] = names.map((hostname, index) => ({
    hostname,
    mac: `a4:83:e7:${(16 + index).toString(16)}:9c:${(32 + index * 7).toString(16).padStart(2, '0')}`,
    ip: index === 6 ? '' : `192.168.8.${100 + index * 9}`,
    signalDbm: [-38, -91, -55, -67, -72, -84, -61, -49, -77, -88, -90, -70, -66, -58][index],
    connectedSeconds: [34_560_000, 5, 59, 3_600, 86_399, 7_200, 45, 120_000, 600, 3_000_000, 1, 18_000, 900, 4_500][
      index
    ],
    band: index % 3 === 0 ? '2.4GHz' : '5GHz',
    downMbps: [942.7, 0, 0.1, 12.5, 99.9, 100, 0, 3.2, 25.6, 0.4, 0, 1.1, 0, 488.2][index],
    upMbps: [88.4, 0, 0, 1.2, 9.9, 10, 0, 0.3, 2.2, 0, 0, 0.1, 0, 41.9][index],
    // From nothing to over a terabyte.
    downBytes: [
      1_234_567_890_123, 0, 512, 99_999_999, 7_340_032_000, 1_000_000_000, 0, 3, 25_600_000, 400, 0, 1_100, 0,
      488_200_000_000,
    ][index],
    upBytes: [88_400_000_000, 0, 0, 1_200_000, 990_000_000, 10_000_000, 0, 0, 2_200_000, 0, 0, 100, 0, 41_900_000_000][
      index
    ],
    // The longest name allowed, and one in another script.
    name:
      index === 6
        ? 'Büro-Drucker im zweiten Stock (Flur, links)'.slice(0, 40)
        : index === 10
          ? '温度传感器'
          : undefined,
    limitReached: index === 9 ? true : undefined,
    internetAccess: index !== 1 && index !== 9,
    pausedSeconds: index === 1 ? 2_592_000 : index === 9 ? 30 : undefined,
    isThisDevice: index === 3,
  }));
  // The last two are on the Ethernet client port: no band, no measured traffic.
  clients.push(
    {
      hostname: 'Aleksandra-Wiśniewska-Kowalczyk-Workstation-Docking-Station',
      mac: 'a4:83:e7:2e:9c:7e',
      ip: '192.168.8.250',
      signalDbm: 0,
      connectedSeconds: 1_209_600,
      band: '',
      wired: true,
      downMbps: 0,
      upMbps: 0,
      downBytes: 0,
      upBytes: 0,
      internetAccess: false,
      pausedSeconds: 600,
    },
    {
      hostname: '',
      mac: 'a4:83:e7:2f:9c:85',
      ip: '',
      signalDbm: 0,
      connectedSeconds: 2,
      band: '',
      wired: true,
      downMbps: 0,
      upMbps: 0,
      downBytes: 0,
      upBytes: 0,
      internetAccess: true,
    },
  );
  status.clients = clients;
  status.guest = {
    enabled: true,
    running: true,
    ssid: guestSsid,
    clients: [
      {
        hostname: 'Aleksandra-Wiśniewska-Kowalczyk-Pixel-8-Pro',
        mac: 'c2:aa:00:11:22:34',
        ip: '192.168.9.254',
        signalDbm: -80,
        connectedSeconds: 59,
        band: '2.4GHz',
        downMbps: 0.1,
        upMbps: 0,
        downBytes: 18_000_000_000,
        upBytes: 950_000_000,
        internetAccess: true,
      },
      {
        hostname: '',
        mac: 'c2:aa:00:11:22:35',
        ip: '192.168.9.21',
        signalDbm: -66,
        connectedSeconds: 3_600,
        band: '5GHz',
        downMbps: 5,
        upMbps: 0.5,
        downBytes: 1,
        upBytes: 0,
        internetAccess: true,
      },
    ],
  };
  router.guestSettings = { ...router.guestSettings, ssid: guestSsid };

  status.throughput = {
    downMbps: 942.7,
    downPeakMbps: 1_203.4,
    upMbps: 88.4,
    upPeakMbps: 451.9,
    peakMbps: 1_284.6,
    sessionBytes: 1_284_000_000_000,
  };
  status.uptimeSeconds = 34_560_000;
  status.system = {
    ...status.system,
    cpuPercent: 99.6,
    cpuModel: 'Broadcom BCM2711, Quad-core Cortex-A72 (ARM v8) 64-bit SoC @ 1.8 GHz',
    ramUsedMb: 1_023,
    ramTotalMb: 1_024,
    swapUsedMb: 512,
    swapTotalMb: 512,
    temperatureC: 87.4,
    thermalState: 'Throttled (soft temperature limit reached)',
    throttling: 'Active since 2 hours',
  };
  status.device = {
    ...status.device,
    // Nearly the longest name RasBeacon takes (63 characters), with no spaces to wrap at.
    hostname: 'travel-router-of-konstantin-oberhauser-wettstein-office-sweden.lan',
    kernel: 'Linux 6.6.31+rpt-rpi-v8 #1 SMP PREEMPT Debian 1:6.6.31-1+rpt1 (2024-05-29) aarch64',
  };
  status.storage = {
    ...status.storage,
    totalGb: 119.1,
    firmwareGb: 3.8,
    userGb: 115.1,
    medium: 'microSD (SanDisk Extreme PRO)',
  };
  status.speedtest.last = {
    server: 'Frankfurt am Main, Germany (FRA)',
    ageSeconds: 3_000_000,
    downloadMbps: 1_284.6,
    uploadMbps: 0.4,
    pingMs: 1_234.5,
  };
  status.update = {
    ...status.update,
    message:
      'Updated to v1.9.3 on 2026-09-30. The previous version (v1.9.2) is kept and starts again by itself if this one fails.',
    latestVersion: 'v1.10.0-rc.2+build.20261011',
    latestUrl: 'https://github.example.com/rasbeacon/rasbeacon/releases/tag/v1.10.0-rc.2',
    // A new version that needs a fresh install, from a refused file: no release page known.
    reflash: { version: 'v1.10.0-rc.2+build.20261011', minFromVersion: 'v1.9.12-beta.4' },
  };
  status.radios = [
    ...status.radios,
    {
      interface: 'wlx00c0cab1c2d3',
      phy: 'phy2',
      driver: 'rtl88x2bu (Realtek RTL8812BU, out-of-tree)',
      isUsb: true,
      usbId: '0bda:b812',
      supportsAp: false,
      maxAps: 1,
      role: 'unused',
    },
  ];

  router.uplinkSettings.networks = [
    { id: 'n1', ssid: uplinkSsid, security: 'open', hidden: false, hasPassword: false },
    {
      id: 'n2',
      ssid: 'eduroam',
      security: 'enterprise',
      hidden: false,
      hasPassword: true,
      enterprise: {
        method: 'tls',
        identity: 'bartholomew.fitzgerald@northwind-industries-holdings.example.com',
        anonymousIdentity: 'anonymous@northwind-industries-holdings.example.com',
        serverName: 'radius-authentication-primary.northwind-industries-holdings.example.com',
        allowNoCertificateCheck: false,
        caCertSummary:
          'Northwind Industries Holdings Internal Root Certification Authority G2 (valid until 2041-12-31), and 2 more',
        clientCertSummary: 'bartholomew.fitzgerald@northwind-industries-holdings.example.com (valid until 2027-03-31)',
        hasClientKey: true,
      },
    },
    { id: 'n3', ssid: 'XFINITYWIFIHOTSPOTPUBLICGUESTNET', security: 'personal', hidden: true, hasPassword: true },
    {
      id: '',
      ssid: '☕ Café Wi-Fi',
      security: 'personal',
      hidden: false,
      hasPassword: false,
      password: 'espresso-lungo',
    },
  ];
  demo.nearbyNetworks = [
    { ssid: uplinkSsid, signalDbm: -91, frequencyMhz: 5745, security: 'open', label: 'Open' },
    {
      ssid: 'XFINITYWIFIHOTSPOTPUBLICGUESTNET',
      signalDbm: -30,
      frequencyMhz: 2412,
      security: 'personal',
      label: 'WPA2/WPA3-Personal',
    },
    {
      ssid: '🏨 Hôtel Château-Frontenac 5G',
      signalDbm: -67,
      frequencyMhz: 5955,
      security: 'enterprise',
      label: 'WPA2/WPA3-Enterprise (192-bit)',
    },
    { ssid: 'J', signalDbm: -99, frequencyMhz: 2462, security: 'other', label: 'WEP' },
  ];

  router.sshSettings.authorizedKeys = [
    'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK0wmN/Cr3JXqmLW7u+g9pTh+wyqDHpSQEIQczXkVx9q owner@laptop',
    `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQ${'Cx8vK2Lq9Tz4WbN7Rj3Pm6Hd1Fs5Ga0Ye8Uc2Vo4Xi7Ql9Bn3Mk6Jp1Rt5Sw8Dz2Ah4Ef7Gi0Ku3Lw6'.repeat(9)} bartholomew.fitzgerald@northwind-industries-holdings.example.com`,
  ].join('\n');
  // Four DNS servers, the most allowed: a long IPv6 address and two long DoH addresses.
  router.dnsSettings = {
    servers: [
      '2a10:50c0::ad1:ff',
      '94.140.14.140',
      'https://dns.adguard-dns.com/dns-query',
      'https://doh.mullvad.net:443/dns-query?ct=application/dns-message',
    ],
    withVpn: true,
  };
  // Plugins in every state, with the longest names and messages.
  const base = router.plugins.plugins[0];
  router.plugins.plugins = [
    ...router.plugins.plugins,
    {
      ...base,
      id: 'home-assistant-companion-bridge-xl',
      name: 'Home Assistant Companion Bridge',
      state: 'failed',
      message:
        "Installing failed: its image can't be downloaded: the image registry answered 429 Too Many Requests (registry-1.docker.io/library/home-assistant-companion-bridge-extended-edition).",
      metrics: [],
      healthy: undefined,
    },
    { ...base, id: 'crashy', name: 'J', state: 'crashed', metrics: [], healthy: undefined },
    {
      // Its files can't be read: no manifest, so no name (its ID instead), icon or settings.
      ...base,
      id: 'old-weather-station',
      name: 'old-weather-station',
      state: 'failed',
      message:
        "Its files can't be read: open /var/lib/rasbeacon/plugins/installed/old-weather-station/manifest.json: no such file or directory. Remove it and install it again.",
      icon: '',
      description: '',
      readme: '',
      afterInstall: '',
      permissions: [],
      ports: [],
      settings: [],
      metrics: [],
      healthy: undefined,
      updateVersion: undefined,
      canUpdate: false,
      broken: true,
    },
    {
      ...base,
      id: 'waiting',
      name: 'Grafana Ünïcode Dashböard für Reisende',
      state: 'waiting',
      message: 'Waiting for the VPN to connect before downloading.',
      metrics: [],
    },
    {
      ...base,
      id: 'metrics',
      name: 'Long numbers',
      metrics: [
        { label: 'Queries in the last 24 hours', value: '1,234,567,890' },
        { label: 'Blocked', value: '100.0%' },
        { label: 'Average answer', value: '1,234\u00a0ms' },
        { label: 'Rules', value: '' },
      ],
    },
  ];

  // The largest data limit, on a metered connection, with two devices over it.
  router.usageSettings = { limitMb: 1_000_000, since: '2024-02-29T23:59:59Z', metered: true, devicesPaused: 2 };
  // A 2 TB SSD instead of an SD card: nearly 1,900 sizes to choose from.
  router.swapSettings = { sizeGb: 0, maxGb: 1_862 };
  router.httpsSettings = {
    ...router.httpsSettings,
    uploaded: true,
    subject: 'CN=rasbeacon-7f3a9c2e4b1d7162.travel.example.org, O=Northwind Industries Holdings, C=DE',
  };
}

/** No devices on either network. */
export function applyNoClients(demo: DemoState) {
  const { status } = demo.router;
  status.clients = [];
  status.guest = { ...status.guest, clients: [] };
  // Nobody to send anything: the live rates are zero, the session's totals stay.
  status.throughput = { ...status.throughput, downMbps: 0, upMbps: 0 };
  // And no room: a card too full for any swap file, and no DNS servers of its own.
  demo.router.swapSettings = { sizeGb: 0, maxGb: 0 };
  demo.router.dnsSettings = { servers: [], withVpn: false };
  demo.router.plugins.plugins = [];
}

/** Exactly one of everything that is counted, for "1 device", "1 guest". */
export function applyOneOfEach(demo: DemoState) {
  const { status } = demo.router;
  status.clients = status.clients.slice(0, 1);
  status.guest = { ...status.guest, clients: status.guest.clients.slice(0, 1) };
  status.warnings = ['The CPU is at 81 °C and RasBeacon has slowed itself down to cool off.'];
  demo.router.vpnSettings.profiles = demo.router.vpnSettings.profiles.slice(0, 1);
  demo.router.uplinkSettings.networks = demo.router.uplinkSettings.networks.slice(0, 1);
  demo.nearbyNetworks = (demo.nearbyNetworks ?? []).slice(0, 1);
  demo.router.swapSettings = { sizeGb: 1, maxGb: 1 };
  demo.router.dnsSettings = { servers: ['1.1.1.1'], withVpn: false };
  demo.router.usageSettings = { limitMb: 1, since: new Date().toISOString(), metered: true, devicesPaused: 1 };
}
