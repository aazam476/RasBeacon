// Reading the device limit and the data limit as typed (src/lib/limits.ts).
import { describe, expect, it } from 'vitest';
import { readDataLimit, readDeviceLimit, showDataLimit } from '@/lib/limits';

describe('device limit', () => {
  it.each([
    ['', 0],
    [' 8 ', 8],
    ['231', 231],
  ])('%j is %d', (text, value) => {
    expect(readDeviceLimit(text)).toEqual({ ok: true, value });
  });
  it.each(['0', '232', '2.5', '-3', 'ten'])('%j is refused', (text) => {
    expect(readDeviceLimit(text).ok).toBe(false);
  });
});

describe('data limit', () => {
  it.each([
    ['', 'GB', 0],
    ['5', 'GB', 5000],
    ['1.5', 'GB', 1500],
    ['1,5', 'GB', 1500], // a decimal comma, as many keyboards type it
    ['750', 'MB', 750],
    ['1000', 'GB', 1_000_000],
  ] as const)('%j %s is %d MB', (text, unit, value) => {
    expect(readDataLimit(text, unit)).toEqual({ ok: true, value });
  });
  it.each([
    ['0', 'GB'],
    ['1001', 'GB'],
    ['0.0001', 'GB'], // rounds to 0 MB
    ['lots', 'MB'],
    ['-1', 'MB'],
  ] as const)('%j %s is refused', (text, unit) => {
    expect(readDataLimit(text, unit).ok).toBe(false);
  });
  it('shows whole GB in GB, other amounts in MB, and no limit as empty', () => {
    expect(showDataLimit(5000)).toEqual({ text: '5', unit: 'GB' });
    expect(showDataLimit(750)).toEqual({ text: '750', unit: 'MB' });
    expect(showDataLimit(0)).toEqual({ text: '', unit: 'GB' });
  });
});
