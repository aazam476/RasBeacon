// The rules for "Always use this address", the same as the router's.
import { describe, expect, it } from 'vitest';
import type { Client } from '@/data/types';
import { fixedAddressProblem } from '@/lib/fixedAddress';

const other = { mac: '8e:00:11:22:33:44', name: 'Pixel-8', fixedAddress: '10.87.0.60' } as Client;

describe('fixed address', () => {
  it.each([
    ['', null],
    ['10.87.0.50', null],
    [' 10.87.0.50 ', null],
    ['10.87.0', 'Enter an address like 10.87.0.2.'],
    ['10.87.0.256', 'Enter an address like 10.87.0.2.'],
    ['10.87.0.05', 'Enter an address like 10.87.0.2.'],
    ['192.168.1.20', 'Use an address from 10.87.0.1 to 10.87.0.254.'],
    ['10.87.0.255', 'Use an address from 10.87.0.1 to 10.87.0.254.'],
    ['10.87.0.0', 'Use an address from 10.87.0.1 to 10.87.0.254.'],
    ['10.87.0.1', "That is RasBeacon's own address."],
    ['10.87.0.60', 'Pixel-8 already uses this address.'],
  ])('%j → %j', (text, problem) => {
    expect(fixedAddressProblem(text, '3a:1f:22:aa:bb:cc', [other])).toBe(problem);
  });

  it('a device may keep its own address, and follows the hotspot’s network', () => {
    expect(fixedAddressProblem('10.87.0.60', other.mac, [other])).toBeNull();
    expect(fixedAddressProblem('10.88.0.20', 'x', [], '10.88.0.1')).toBeNull();
  });

  it('names a device without a name by what it calls itself, or its MAC address', () => {
    const unnamed = { ...other, name: undefined, hostname: '' } as Client;
    expect(fixedAddressProblem('10.87.0.60', 'x', [unnamed])).toBe('8e:00:11:22:33:44 already uses this address.');
  });
});
