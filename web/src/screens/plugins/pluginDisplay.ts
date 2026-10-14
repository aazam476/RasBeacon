import { FieldErrors } from '@/data/errors';
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
}

/** The form's starting values: the ports and settings it has (or suggests), passwords empty. */
export function draftOf(ports: PluginPort[], settings: PluginSetting[], drive?: string): ChoicesDraft {
  return {
    ports: Object.fromEntries(ports.map((port) => [port.key, String(port.host)])),
    settings: Object.fromEntries(settings.map((s) => [s.key, s.type === 'password' ? '' : s.value])),
    drive: drive ?? '',
  };
}

/** The draft as the router takes it. */
export function choicesOf(draft: ChoicesDraft): PluginChoices {
  return {
    ports: Object.fromEntries(Object.entries(draft.ports).map(([key, value]) => [key, Number(value.trim()) || 0])),
    settings: draft.settings,
    drive: draft.drive,
  };
}

/** A metric's "14 ms" as the number and its unit, for the large figures. */
export function splitMetric(value: string): { value: string; unit?: string } {
  if (value === '') return { value: '—' };
  const [number, unit] = value.split(' ');
  return unit ? { value: number, unit } : { value };
}

/** "Version 1.2 · by RasBeacon" */
export function byline(plugin: Pick<Plugin, 'version' | 'author'>): string {
  return plugin.author ? `Version ${plugin.version} · by ${plugin.author}` : `Version ${plugin.version}`;
}

/** What went wrong, in words: a field's message, or the error's. */
export function errorText(problem: unknown): string {
  if (problem instanceof FieldErrors) return Object.values(problem.fields)[0] ?? problem.message;
  return problem instanceof Error ? problem.message : String(problem);
}
