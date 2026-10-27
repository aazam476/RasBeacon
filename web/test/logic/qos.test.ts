// Priority devices: the speeds as typed (Mb/s) and as the router keeps them (kbit/s).
import { describe, expect, it } from 'vitest';
import { kbpsFromText, speedProblem, textFromKbps } from '@/lib/qos';

describe('internet speeds', () => {
  it.each([
    ['', 0],
    ['  ', 0],
    ['50', 50_000],
    ['12.5', 12_500],
    ['12,5', 12_500],
    ['0.0004', 0],
  ])('reads %j as %d kbit/s', (text, kbps) => {
    expect(kbpsFromText(text)).toBe(kbps);
  });

  it.each(['fast', '-5', '1e3', '5 Mb/s'])('finds %j isn’t a speed', (text) => {
    expect(speedProblem(kbpsFromText(text))).toBeDefined();
  });

  it('accepts 0 (not set) and 1 Mb/s to 10 Gb/s', () => {
    for (const ok of [0, 1_000, 10_000_000]) expect(speedProblem(ok)).toBeUndefined();
    for (const bad of [999, 10_000_001, 1.5]) expect(speedProblem(bad)).toBeDefined();
  });

  it('shows a speed as typed', () => {
    expect(textFromKbps(0)).toBe('');
    expect(textFromKbps(100_000)).toBe('100');
    expect(textFromKbps(20_500)).toBe('20.5');
    expect(textFromKbps(94_370)).toBe('94.4');
  });
});
