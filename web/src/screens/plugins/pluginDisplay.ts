import type { BaseIconName, Tone } from '@/ui';
import type { Plugin, PluginChoices, PluginPort, PluginSetting, PluginState } from '@/data/types';

/** A plugin's state in a word or two, and the badge color for it. */
export const STATE_LABELS: Record<PluginState, { text: string; tone: Tone }> = {
  installing: { text: 'Installing', tone: 'tint' },
  waiting: { text: 'Waiting for the VPN', tone: 'orange' },
  starting: { text: 'Starting', tone: 'tint' },
  running: { text: 'Running', tone: 'green' },
  stopped: { text: 'Stopped', tone: 'gray' },
  crashed: { text: 'Stopped after crashing', tone: 'red' },
  failed: { text: 'Not working', tone: 'red' },
};

/** The symbol for each permission, as in a phone's permission prompts. */
export const PERMISSION_ICONS: Record<string, BaseIconName> = {
  internet: 'public',
  'lan-access': 'devices',
  'usb-drive': 'usb',
  i2c: 'memory',
  gpio: 'developer_board',
  'power-button': 'restart_alt',
  temperature: 'device_thermostat',
};

/** True while the router is still working on it (the page follows along). */
export function isBusy(state: PluginState): boolean {
  return state === 'installing' || state === 'waiting' || state === 'starting';
}

/** Where a plugin came from: "From a file", or its GitHub repository. */
export function sourceLabel(source: string): string {
  return source === 'file' ? 'From a file' : source;
}

/** The form's values as typed: numbers and switches are text until they are sent. */
export interface ChoicesDraft {
  ports: Record<string, string>;
  settings: Record<string, string>;
  drive: string;
  /** The size of its data in GB as typed; absent for a plugin whose size is fixed. */
  storage?: string;
}

/** Megabytes in a gigabyte, as the router counts a plugin's storage. */
const MB_PER_GB = 1024;

/**
 * A size in MB as the GB the storage field shows, to a tenth: 2048 → "2", 1536 → "1.5". `round`
 * picks the way: `up` for a smallest size and `down` for a largest, so the size shown is always
 * one RasBeacon takes.
 */
export function storageGbText(mb: number, round: 'nearest' | 'up' | 'down' = 'nearest'): string {
  const tenths = (mb / MB_PER_GB) * 10;
  const rounded = round === 'up' ? Math.ceil(tenths) : round === 'down' ? Math.floor(tenths) : Math.round(tenths);
  return String(rounded / 10);
}

/**
 * The form's starting values: the ports and settings it has (or suggests), passwords empty, and
 * the size of its data when the owner may choose it (`storageMb`, left out otherwise).
 */
export function draftOf(
  ports: PluginPort[],
  settings: PluginSetting[],
  drive?: string,
  storageMb?: number,
): ChoicesDraft {
  return {
    ports: Object.fromEntries(ports.map((port) => [port.key, String(port.host)])),
    settings: Object.fromEntries(settings.map((s) => [s.key, s.type === 'password' ? '' : s.value])),
    drive: drive ?? '',
    storage: storageMb === undefined ? undefined : storageGbText(storageMb),
  };
}

/** The draft as the router takes it; the size only for a plugin that lets the owner choose it. */
export function choicesOf(draft: ChoicesDraft): PluginChoices {
  const choices: PluginChoices = {
    ports: Object.fromEntries(Object.entries(draft.ports).map(([key, value]) => [key, Number(value.trim()) || 0])),
    settings: draft.settings,
    drive: draft.drive,
  };
  if (draft.storage !== undefined) {
    // A size that isn't a number is sent as 1 MB, so the router says what sizes it takes.
    const gb = Number(draft.storage.trim().replace(',', '.'));
    choices.storageMb = Number.isFinite(gb) && gb > 0 ? Math.round(gb * MB_PER_GB) : 1;
  }
  return choices;
}

/**
 * A metric's "14 ms" as the number and its unit, for the large figures. RasBeacon writes a
 * non-breaking space (\u00a0) before a unit, so only that space splits: a text metric with
 * ordinary spaces ("All good") stays whole.
 */
export function splitMetric(value: string): { value: string; unit?: string } {
  if (value === '') return { value: '—' };
  const [number, unit] = value.split('\u00a0');
  return unit ? { value: number, unit } : { value };
}

/** The text color for a state's tone, as on the badges. */
export function toneText(tone: Tone): string {
  switch (tone) {
    case 'green':
      return 'text-green';
    case 'red':
      return 'text-red';
    case 'orange':
      return 'text-orange';
    case 'tint':
      return 'text-tint';
    default:
      return '';
  }
}

/** What a number setting takes, next to its label: "(°C, 0 to 85)", "(at least 1)". */
export function numberNote(setting: Pick<PluginSetting, 'unit' | 'min' | 'max'>): string | undefined {
  const { unit, min, max } = setting;
  let range = '';
  if (min !== undefined && max !== undefined) range = `${min} to ${max}`;
  else if (min !== undefined) range = `at least ${min}`;
  else if (max !== undefined) range = `at most ${max}`;
  const parts = [unit, range].filter(Boolean);
  return parts.length > 0 ? `(${parts.join(', ')})` : undefined;
}

/** "Version 1.2 · by RasBeacon" */
export function byline(plugin: Pick<Plugin, 'version' | 'author'>): string {
  return plugin.author ? `Version ${plugin.version} · by ${plugin.author}` : `Version ${plugin.version}`;
}
