// How the plugin pages show versions, metrics, settings and errors.
import { describe, expect, it } from 'vitest';
import { FieldErrors } from '@/data/errors';
import type { PluginPort, PluginSetting } from '@/data/types';
import {
  choicesOf,
  draftOf,
  errorText,
  isNewerVersion,
  numberNote,
  splitMetric,
  toneText,
} from '@/screens/plugins/pluginDisplay';

describe('versions', () => {
  it.each([
    ['1.10.0', '1.9.2', true],
    ['0.107.79-2', '0.107.79-1', true],
    ['v2.0', '2.0', false],
    ['1.0', '1.0.1', false],
  ])('%s is newer than %s: %s', (a, b, newer) => {
    expect(isNewerVersion(a, b)).toBe(newer);
  });
});

describe('metrics', () => {
  it('splits a number from its unit at the non-breaking space only', () => {
    expect(splitMetric('14 ms')).toEqual({ value: '14', unit: 'ms' });
    expect(splitMetric('All good')).toEqual({ value: 'All good' });
    expect(splitMetric('')).toEqual({ value: '—' });
  });
});

describe('settings form', () => {
  it('starts with passwords empty and sends ports as numbers', () => {
    const draft = draftOf(
      [{ key: 'tcp/3000', name: 'Web', host: 3000 } as PluginPort],
      [
        { key: 'password', type: 'password', value: 'secret' } as PluginSetting,
        { key: 'upstream', type: 'select', value: 'quad9' } as PluginSetting,
      ],
    );
    expect(draft).toEqual({ ports: { 'tcp/3000': '3000' }, settings: { password: '', upstream: 'quad9' }, drive: '' });
    expect(choicesOf({ ...draft, ports: { 'tcp/3000': ' 8080 ' } }).ports).toEqual({ 'tcp/3000': 8080 });
  });

  it('describes a number setting’s range', () => {
    expect(numberNote({ unit: '°C', min: 0, max: 85 })).toBe('(°C, 0 to 85)');
    expect(numberNote({ min: 1 })).toBe('(at least 1)');
    expect(numberNote({})).toBeUndefined();
  });
});

describe('errors', () => {
  it('shows a field’s message, or the error’s', () => {
    expect(errorText(new FieldErrors({ port: 'Port 443 is RasBeacon’s.' }))).toBe('Port 443 is RasBeacon’s.');
    expect(errorText(new Error('No room on the card.'))).toBe('No room on the card.');
  });
});

describe('state colors', () => {
  it.each([
    ['green', 'text-green'],
    ['red', 'text-red'],
    ['orange', 'text-orange'],
    ['tint', 'text-tint'],
    ['gray', ''],
  ] as const)('%s text is "%s"', (tone, classes) => {
    expect(toneText(tone)).toBe(classes);
  });
});
