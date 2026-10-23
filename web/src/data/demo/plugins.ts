import type {
  Plugin,
  PluginCatalogEntry,
  PluginChoices,
  PluginDrive,
  PluginKeptData,
  PluginLogLine,
  PluginPermission,
  PluginPreview,
  PluginSetting,
  PluginsState,
} from '../types';

/**
 * The demo's plugins: the three in RasBeacon's own catalog (AdGuard Home, Samba, Argon ONE V2)
 * and one from another source, with icons drawn here as SVG (the real ones are PNG files in
 * each .rba).
 */

function svgIcon(background: string, glyph: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${background}" stop-opacity="0.85"/><stop offset="1" stop-color="${background}"/></linearGradient></defs><rect width="128" height="128" rx="28" fill="url(#g)"/>${glyph}</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

export const PLUGIN_ICONS = {
  adguard: svgIcon(
    '#2f9e5b',
    '<path d="M64 24 32 36v24c0 22 14 38 32 44 18-6 32-22 32-44V36z" fill="#fff"/><path d="m50 64 10 10 20-22" fill="none" stroke="#2f9e5b" stroke-width="9" stroke-linecap="round" stroke-linejoin="round"/>',
  ),
  samba: svgIcon(
    '#2a6fdb',
    '<path d="M26 40a8 8 0 0 1 8-8h20l8 8h32a8 8 0 0 1 8 8v36a8 8 0 0 1-8 8H34a8 8 0 0 1-8-8z" fill="#fff"/><circle cx="64" cy="66" r="6" fill="#2a6fdb"/><path d="M64 72v12M52 84h24" stroke="#2a6fdb" stroke-width="5" stroke-linecap="round"/>',
  ),
  argon: svgIcon(
    '#3a3a3c',
    '<circle cx="64" cy="64" r="34" fill="none" stroke="#fff" stroke-width="6"/><circle cx="64" cy="64" r="7" fill="#fff"/><path d="M64 57c-4-12 0-22 8-24 6 4 6 14-8 24zM71 64c12-4 22 0 24 8-4 6-14 6-24-8zM64 71c4 12 0 22-8 24-6-4-6-14 8-24zM57 64c-12 4-22 0-24-8 4-6 14-6 24 8z" fill="#fff"/>',
  ),
  notes: svgIcon(
    '#e8a21a',
    '<rect x="36" y="28" width="56" height="72" rx="8" fill="#fff"/><path d="M48 50h32M48 64h32M48 78h20" stroke="#e8a21a" stroke-width="6" stroke-linecap="round"/>',
  ),
};

export const PERMISSIONS: Record<string, PluginPermission> = {
  internet: { id: 'internet', title: 'Use the internet', detail: 'Through the VPN when it is on, like your devices.' },
  'lan-access': {
    id: 'lan-access',
    title: 'Reach your devices',
    detail: 'Start connections to devices on your network (not guests).',
  },
  'usb-drive': {
    id: 'usb-drive',
    title: 'Use a USB drive',
    detail: 'Only the drive you pick, mounted so nothing on it can run.',
  },
  i2c: { id: 'i2c', title: 'Use the I²C bus', detail: "Talk to chips on the board, such as a case's fan controller." },
  gpio: { id: 'gpio', title: 'Use the GPIO pins', detail: "Read and set the pins on the Pi's header." },
  'power-button': {
    id: 'power-button',
    title: 'Shut down or restart RasBeacon',
    detail: "Ask RasBeacon to shut down or restart, for a case's power button.",
  },
  temperature: { id: 'temperature', title: 'Read the temperature', detail: "Read the processor's temperature." },
};

const BUILT_IN = 'aazam476/RasBeacon-plugins';

/** Everything the demo knows about a plugin, installed or not. */
interface PluginDefinition {
  id: string;
  file: string;
  source: string;
  name: string;
  version: string;
  description: string;
  author: string;
  homepage?: string;
  icon: string;
  readme: string;
  signed: boolean;
  keyId?: string;
  permissions: string[];
  ports: Plugin['ports'];
  settings: PluginSetting[];
  usesDrive: boolean;
  driveOptional?: boolean;
  afterInstall?: string;
  metricLabels: string[];
  memoryMb: number;
  cpus: number;
  storageMb: number;
  /** Bytes still to download when installing (the image from its registry). */
  downloadSize: number;
  size: number;
  /** Boards it runs on (empty: all). */
  boards: string[];
}

const fanSetting = (key: string, label: string, value: string, unit: string, max: number): PluginSetting => ({
  key,
  label,
  type: 'number',
  value,
  unit,
  min: 0,
  max,
});

export const PLUGIN_DEFINITIONS: PluginDefinition[] = [
  {
    id: 'adguard-home',
    file: 'adguard-home.rba',
    source: BUILT_IN,
    name: 'AdGuard Home',
    version: '0.107.57-1',
    description: 'Blocks ads and trackers for every device on your hotspot, and shows what they look up.',
    author: 'RasBeacon',
    homepage: 'https://github.com/AdguardTeam/AdGuardHome',
    icon: PLUGIN_ICONS.adguard,
    // The README files are the real plugins' (Markdown, wrapped as the files are).
    readme:
      "AdGuard Home answers your devices' DNS questions and blocks the ones for ads,\ntrackers and malware. It asks **Quad9** over DNS-over-HTTPS for everything\nelse, through the VPN when it is on.\n\n**After installing:** on RasBeacon's Internet page, set the DNS servers to\nthe address shown under *Next steps*, and turn on **Use these servers while the\nVPN is on**. Guests' lookups are filtered too, but only your devices can open\nAdGuard Home's web interface.\n\nIn AdGuard Home all questions come from RasBeacon (it asks on your devices'\nbehalf), so its per-device lists show one client.\n\nAdGuard Home is made by AdGuard (GPL-3.0); this plugin runs their official\nimage, pinned to one version.",
    signed: true,
    keyId: '7F3A 9C2E 4B1D 7162',
    permissions: ['internet'],
    ports: [{ key: 'tcp/3000', name: 'Web interface', protocol: 'tcp', container: 3000, host: 3000 }],
    settings: [
      { key: 'username', label: 'Admin username', type: 'text', value: 'admin', required: true },
      {
        key: 'password',
        label: 'Admin password',
        type: 'password',
        value: '',
        required: true,
        help: "For AdGuard Home's web interface. If you change it there, change it here too, for the numbers on this page.",
      },
    ],
    usesDrive: false,
    afterInstall:
      'Set the DNS servers on the Internet page to {address}, and turn on "Use these servers while the VPN is on". Then open AdGuard Home to choose its block lists.',
    metricLabels: ['Queries today', 'Blocked', 'Average answer', 'Rules'],
    memoryMb: 256,
    cpus: 0.5,
    storageMb: 512,
    downloadSize: 21_400_000,
    size: 48_000,
    boards: [],
  },
  {
    id: 'samba',
    file: 'samba.rba',
    source: BUILT_IN,
    name: 'Samba',
    version: '4.20.5-1',
    description: 'Shares a folder, or a USB drive, with the computers on your hotspot.',
    author: 'RasBeacon',
    icon: PLUGIN_ICONS.samba,
    readme:
      "Shares files with the Windows, macOS and Linux computers on your hotspot.\n\nPick a **USB drive** to share it (exFAT, FAT32, NTFS or ext4), or none to use\n2 GB of RasBeacon's own storage. Nothing on the drive can run on RasBeacon.\n\n- **Mac:** Finder → Go → Connect to Server, `smb://<RasBeacon's address>`.\n- **Windows:** File Explorer, `\\\\<RasBeacon's address>` (Windows needs port\n  445).\n\nGuests can't reach it.",
    signed: true,
    keyId: '7F3A 9C2E 4B1D 7162',
    permissions: ['usb-drive'],
    ports: [{ key: 'tcp/445', name: 'File sharing (SMB)', protocol: 'tcp', container: 445, host: 445 }],
    settings: [
      { key: 'share', label: 'Share name', type: 'text', value: 'Files', required: true },
      { key: 'username', label: 'Username', type: 'text', value: 'traveler', required: true },
      { key: 'password', label: 'Password', type: 'password', value: '', required: true },
    ],
    usesDrive: true,
    driveOptional: true,
    afterInstall: 'Connect to smb://{host} (port {port:445}) and log in with the username and password you chose.',
    metricLabels: ['Connected', 'Free space'],
    memoryMb: 128,
    cpus: 0.5,
    storageMb: 2048,
    downloadSize: 0,
    size: 18_900_000,
    boards: [],
  },
  {
    id: 'argon-one',
    file: 'argon-one-v2.rba',
    source: BUILT_IN,
    name: 'Argon ONE V2',
    version: '1.0.0',
    description: "Runs the Argon ONE V2 case's fan by temperature, and its power button.",
    author: 'RasBeacon',
    icon: PLUGIN_ICONS.argon,
    readme:
      "Runs the fan of the **Argon ONE V2** case (Raspberry Pi 4) and its power button.\n\n- **Fan:** three steps. From *Fan on at* the fan runs at the first speed, from\n  *Faster at* at the second, from *Fastest at* at the third. It slows down\n  again 3 °C below a step, so it doesn't switch back and forth.\n- **Power button:** press it twice to restart RasBeacon; hold it for 3 seconds\n  to shut it down. Holding it for 5 seconds cuts the power at once (the case\n  does that by itself).\n\nWhen the plugin is stopped, the fan keeps its last speed.",
    signed: true,
    keyId: '7F3A 9C2E 4B1D 7162',
    permissions: ['i2c', 'gpio', 'power-button', 'temperature'],
    ports: [],
    settings: [
      fanSetting('temp1', 'Fan on at', '55', '°C', 85),
      fanSetting('speed1', 'Speed', '10', '%', 100),
      fanSetting('temp2', 'Faster at', '60', '°C', 85),
      fanSetting('speed2', 'Speed', '55', '%', 100),
      fanSetting('temp3', 'Full speed at', '65', '°C', 85),
    ],
    usesDrive: false,
    metricLabels: ['Fan', 'Temperature'],
    memoryMb: 32,
    cpus: 0.1,
    storageMb: 16,
    downloadSize: 0,
    size: 2_100_000,
    boards: ['pi4'],
  },
  {
    id: 'trip-notes',
    file: 'trip-notes.rba',
    source: 'marta-k/travel-plugins',
    name: 'Trip Notes',
    version: '0.3.1',
    description: 'A shared notepad for everyone on the trip.',
    author: 'Marta K.',
    icon: PLUGIN_ICONS.notes,
    readme: 'A small web page to write down flight times, addresses and plans.',
    signed: false,
    permissions: [],
    ports: [{ key: 'tcp/8080', name: 'Notepad', protocol: 'tcp', container: 8080, host: 8080 }],
    settings: [],
    usesDrive: false,
    metricLabels: [],
    memoryMb: 64,
    cpus: 0.25,
    storageMb: 64,
    downloadSize: 0,
    size: 9_400_000,
    boards: [],
  },
];

export function definitionOf(id: string): PluginDefinition | undefined {
  return PLUGIN_DEFINITIONS.find((definition) => definition.id === id);
}

const LAN_ADDRESS = '192.168.8.1';

function expand(text: string | undefined, plugin: { address: string; ports: Plugin['ports'] }): string | undefined {
  return text
    ?.replaceAll('{address}', plugin.address)
    .replaceAll('{host}', LAN_ADDRESS)
    .replace(/\{port:(\d+)\}/g, (match, port: string) => {
      const found = plugin.ports.find((p) => p.container === Number(port));
      return found ? String(found.host) : match;
    });
}

/** An installed plugin as the router describes it. */
export function installedPlugin(
  definition: PluginDefinition,
  address: string,
  choices: PluginChoices,
  state: Plugin['state'],
): Plugin {
  const ports = definition.ports.map((port) => {
    const host = choices.ports[port.key] ?? port.host;
    return {
      ...port,
      host,
      url: port.protocol === 'tcp' && port.container !== 445 ? `http://${LAN_ADDRESS}:${host}/` : undefined,
    };
  });
  const settings = definition.settings.map((setting) =>
    setting.type === 'password'
      ? { ...setting, value: '', hasValue: Boolean(choices.settings[setting.key]) || setting.hasValue }
      : { ...setting, value: choices.settings[setting.key] ?? setting.value },
  );
  return {
    id: definition.id,
    name: definition.name,
    version: definition.version,
    description: definition.description,
    author: definition.author,
    homepage: definition.homepage,
    icon: definition.icon,
    readme: expand(definition.readme, { address, ports }),
    source: definition.source,
    signed: definition.signed,
    keyId: definition.keyId,
    state,
    healthy: state === 'running' ? true : undefined,
    address,
    permissions: definition.permissions.map((id) => PERMISSIONS[id]),
    ports,
    settings,
    usesDrive: definition.usesDrive,
    driveOptional: definition.driveOptional,
    drive: choices.drive || undefined,
    afterInstall: expand(definition.afterInstall, { address, ports }),
    metrics:
      state === 'running' ? demoMetrics(definition.id) : definition.metricLabels.map((label) => ({ label, value: '' })),
    memoryMb: definition.memoryMb,
    cpus: definition.cpus,
    storageMb: definition.storageMb,
    canUpdate: false,
  };
}

/** Plausible numbers for a running plugin. */
export function demoMetrics(id: string): Plugin['metrics'] {
  switch (id) {
    case 'adguard-home':
      return [
        { label: 'Queries today', value: '12,481' },
        { label: 'Blocked', value: '18.4%' },
        { label: 'Average answer', value: '14 ms' },
        { label: 'Rules', value: '98,213' },
      ];
    case 'samba':
      return [
        { label: 'Connected', value: '1' },
        { label: 'Free space', value: '41.2 GB' },
      ];
    case 'argon-one':
      return [
        { label: 'Fan', value: '10%' },
        { label: 'Temperature', value: '56 °C' },
      ];
    default:
      return [];
  }
}

/** The sample router's plugins: AdGuard Home running, Samba stopped with a drive. */
export function createMockPlugins(): PluginsState {
  const adguard = installedPlugin(
    definitionOf('adguard-home')!,
    '10.89.0.2',
    { ports: {}, settings: { password: 'x' }, drive: '' },
    'running',
  );
  // An older version is installed: the catalog has the newer one.
  adguard.version = '0.107.55-1';
  adguard.updateVersion = '0.107.57-1';
  adguard.canUpdate = true;
  const samba = installedPlugin(
    definitionOf('samba')!,
    '10.89.0.3',
    { ports: {}, settings: { password: 'x' }, drive: '6A1F-22C9' },
    'stopped',
  );
  return {
    plugins: [adguard, samba],
    sources: [
      { source: BUILT_IN, builtIn: true },
      { source: 'marta-k/travel-plugins', builtIn: false },
    ],
    board: 'pi4',
    memoryMb: 3_884,
    swapMb: 2_048,
    metered: false,
    checkedAt: '2026-10-13T08:12:00Z',
  };
}

/** The catalog entries for the installed plugins and the board. */
export function catalogEntries(state: PluginsState): PluginCatalogEntry[] {
  return PLUGIN_DEFINITIONS.filter((definition) => state.sources.some((s) => s.source === definition.source)).map(
    (definition) => {
      const installedVersion = state.plugins.find((p) => p.id === definition.id)?.version;
      return {
        source: definition.source,
        file: definition.file,
        id: definition.id,
        name: definition.name,
        version: definition.version,
        description: definition.description,
        permissions: definition.permissions,
        size: definition.size,
        downloadSize: definition.downloadSize,
        icon: definition.icon,
        installedVersion,
        // The demo's catalog versions are never older than the installed ones, so a different
        // version is a newer one (the router compares them properly).
        updateAvailable: installedVersion !== undefined && installedVersion !== definition.version,
        runsHere: definition.boards.length === 0 || definition.boards.includes(state.board),
      };
    },
  );
}

/**
 * The install sheet's contents for a plugin (an update when it is installed). `kept` is data kept
 * from an earlier install of it that the sheet asks about.
 */
export function previewOf(
  definition: PluginDefinition,
  state: PluginsState,
  stagingId: string,
  kept?: PluginKeptData,
): PluginPreview {
  const installed = state.plugins.find((p) => p.id === definition.id);
  const taken = new Set(
    state.plugins.filter((p) => p.id !== definition.id).flatMap((p) => p.ports.map((port) => port.host)),
  );
  const ports = definition.ports.map((port) => {
    let host = installed?.ports.find((p) => p.key === port.key)?.host ?? port.host;
    while (taken.has(host) || [22, 53, 67, 80, 443].includes(host)) host += 1;
    return { ...port, host };
  });
  const warnings =
    state.memoryMb < 1024
      ? [
          `This Pi has little memory (${state.memoryMb} MB). ${definition.name} may use up to ${definition.memoryMb} MB, which can slow RasBeacon down.`,
        ]
      : [];
  return {
    stagingId,
    keptData: installed ? undefined : kept,
    id: definition.id,
    name: definition.name,
    version: definition.version,
    description: definition.description,
    author: definition.author,
    homepage: definition.homepage,
    icon: definition.icon,
    source: definition.source,
    signed: definition.signed,
    keyId: definition.keyId,
    update: Boolean(installed),
    fromVersion: installed?.version,
    permissions: definition.permissions.map((id) => PERMISSIONS[id]),
    newPermissions: installed
      ? definition.permissions.filter((id) => !installed.permissions.some((p) => p.id === id))
      : undefined,
    ports,
    settings: installed?.settings ?? definition.settings,
    usesDrive: definition.usesDrive,
    driveOptional: definition.driveOptional,
    drive: installed?.drive,
    downloadSize: definition.downloadSize,
    metered: state.metered,
    memoryMb: definition.memoryMb,
    cpus: definition.cpus,
    storageMb: definition.storageMb,
    warnings,
  };
}

export const DEMO_DRIVES: PluginDrive[] = [
  { uuid: '6A1F-22C9', label: 'TRAVEL', fsType: 'exfat', size: 64_000_000_000, usedBy: 'Samba' },
  { uuid: '0d8e5b2a-4f3c-4c1e-9d55-81b0c6f2e9a7', label: '', fsType: 'ext4', size: 500_107_862_016 },
];

export const DEMO_LOG: PluginLogLine[] = [
  { time: '2026-10-19T08:12:03Z', text: '[info] AdGuard Home, version v0.107.57' },
  { time: '2026-10-19T08:12:03Z', text: '[info] home: this is the first launch of AdGuard Home' },
  { time: '2026-10-19T08:12:04Z', text: '[info] dnsproxy: starting dns proxy server' },
  { time: '2026-10-19T08:12:04Z', text: '[info] Upstream 0: https://dns.quad9.net:443/dns-query' },
  { time: '2026-10-19T08:12:04Z', text: '[info] dnsproxy: entering listener loop proto=udp addr=0.0.0.0:53' },
  { time: '2026-10-19T08:12:05Z', text: '[info] go to http://127.0.0.1:3000' },
  { time: '2026-10-19T08:14:41Z', text: '[info] filtering: updated filter 1: 98213 rules' },
];
