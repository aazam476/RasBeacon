// Port forwarding rules: reading the ports typed, showing a rule, and the router's checks.
import { describe, expect, it } from 'vitest';
import type { PortForwardRule } from '@/data/types';
import { formatPorts, parsePorts, portsOverlap, portsText, ruleProblems, ruleSummary } from '@/lib/portForward';

/** A rule to a device, changed by `change`. */
function rule(change: Partial<PortForwardRule> = {}): PortForwardRule {
  return {
    id: 'r',
    name: 'Game',
    protocol: 'tcp',
    outsidePort: 25565,
    mac: 'a4:83:e7:12:34:56',
    enabled: true,
    ...change,
  };
}

describe('parsePorts', () => {
  it.each([
    ['80', { from: 80, to: 80 }],
    ['27015-27030', { from: 27015, to: 27030 }],
    [' 27015 – 27030 ', { from: 27015, to: 27030 }],
    ['65535', { from: 65535, to: 65535 }],
  ])('reads %j', (text, ports) => {
    expect(parsePorts(text)).toEqual(ports);
  });

  it.each([
    ['', 'Use a port from 1 to 65535.'],
    ['0', 'Use a port from 1 to 65535.'],
    ['65536', 'Use a port from 1 to 65535.'],
    ['http', 'Use a port from 1 to 65535.'],
    ['1-2-3', 'Use a port from 1 to 65535.'],
    ['30-20', 'The range must end on a port after the first, up to 65535.'],
    ['30-30', 'The range must end on a port after the first, up to 65535.'],
    ['30-70000', 'The range must end on a port after the first, up to 65535.'],
  ])('refuses %j', (text, problem) => {
    expect(parsePorts(text)).toBe(problem);
  });
});

describe('showing a rule', () => {
  it('writes one port or a range', () => {
    expect(formatPorts(rule())).toBe('25565');
    expect(formatPorts(rule({ outsidePort: 27015, outsidePortEnd: 27030 }))).toBe('27015–27030');
    expect(portsText(rule({ outsidePort: 27015, outsidePortEnd: 27030 }))).toBe('27015-27030');
  });

  it('sums it up with its protocol and inside port', () => {
    expect(ruleSummary(rule())).toBe('TCP 25565');
    expect(ruleSummary(rule({ protocol: 'both', outsidePort: 8080, insidePort: 80 }))).toBe('TCP and UDP 8080 → 80');
    expect(ruleSummary(rule({ protocol: 'udp', insidePort: 25565 }))).toBe('UDP 25565');
  });
});

describe('portsOverlap', () => {
  it('needs a port and a protocol in common', () => {
    const range = rule({ outsidePort: 27015, outsidePortEnd: 27030 });
    expect(portsOverlap(range, rule({ outsidePort: 27030 }))).toBe(true);
    expect(portsOverlap(range, rule({ outsidePort: 27031 }))).toBe(false);
    expect(portsOverlap(range, rule({ outsidePort: 27020, protocol: 'udp' }))).toBe(false);
    expect(portsOverlap(range, rule({ outsidePort: 27020, protocol: 'both' }))).toBe(true);
  });
});

describe('ruleProblems', () => {
  it('finds nothing wrong with a good rule', () => {
    expect(ruleProblems(rule(), [])).toEqual({});
  });

  it('asks for a name of up to 40 characters', () => {
    expect(ruleProblems(rule({ name: '  ' }), []).name).toBeTruthy();
    expect(ruleProblems(rule({ name: 'x'.repeat(41) }), []).name).toBeTruthy();
  });

  it('keeps the ports RasBeacon gets its address on', () => {
    const problem = 'RasBeacon needs UDP ports 67 and 68 to get its own address.';
    expect(ruleProblems(rule({ protocol: 'udp', outsidePort: 68 }), []).outsidePort).toBe(problem);
    expect(ruleProblems(rule({ protocol: 'both', outsidePort: 60, outsidePortEnd: 70 }), []).outsidePort).toBe(problem);
    expect(ruleProblems(rule({ protocol: 'tcp', outsidePort: 67 }), []).outsidePort).toBeUndefined();
  });

  it('refuses a port another enabled rule forwards, unless one of them is off', () => {
    const other = rule({ id: 'o', name: 'Server' });
    expect(ruleProblems(rule(), [other]).outsidePort).toBe('"Server" already forwards this port.');
    expect(ruleProblems(rule({ enabled: false }), [other]).outsidePort).toBeUndefined();
    expect(ruleProblems(rule(), [{ ...other, enabled: false }]).outsidePort).toBeUndefined();
  });

  it('keeps a range’s ports', () => {
    const range = rule({ outsidePort: 27015, outsidePortEnd: 27030 });
    expect(ruleProblems({ ...range, insidePort: 80 }, []).insidePort).toBeTruthy();
    expect(ruleProblems({ ...range, insidePort: 27015 }, []).insidePort).toBeUndefined();
    expect(ruleProblems(rule({ insidePort: 80 }), []).insidePort).toBeUndefined();
  });

  it('needs somewhere to go', () => {
    expect(ruleProblems(rule({ mac: undefined }), []).target).toBe('Choose a device or an installed plugin.');
    expect(ruleProblems(rule({ mac: undefined, plugin: 'samba' }), []).target).toBeUndefined();
  });
});
