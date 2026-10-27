import type { PortForwardProtocol, PortForwardRule } from '@/data/types';

/** The protocol menu's choices. */
export const PROTOCOL_OPTIONS: readonly { value: PortForwardProtocol; label: string }[] = [
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' },
  { value: 'both', label: 'TCP and UDP' },
];

/** The most rules the router keeps (internal/agent/portforward.go). */
export const MAX_PORT_FORWARDS = 50;

/** One port or a range of ports, as typed in the "Outside port" field. */
export interface PortRange {
  from: number;
  /** The same as `from` for one port. */
  to: number;
}

/** A whole number from 1 to 65535, or null. */
function port(text: string): number | null {
  if (!/^\d{1,5}$/.test(text)) return null;
  const value = Number(text);
  return value >= 1 && value <= 65535 ? value : null;
}

/**
 * Reads "8080" or a range such as "27015-27030" (spaces and an en dash are fine too). Answers
 * the ports, or the reason they can't be used, in the router's words.
 */
export function parsePorts(text: string): PortRange | string {
  const [first, last, ...rest] = text.split(/\s*[-–]\s*/).map((part) => part.trim());
  const from = port(first);
  if (from === null || rest.length > 0) return 'Use a port from 1 to 65535.';
  if (last === undefined) return { from, to: from };
  const to = port(last);
  if (to === null || to <= from) return 'The range must end on a port after the first, up to 65535.';
  return { from, to };
}

/** "8080", or "27015–27030" for a range. */
export function formatPorts(rule: Pick<PortForwardRule, 'outsidePort' | 'outsidePortEnd'>): string {
  const end = rule.outsidePortEnd ?? 0;
  return end > rule.outsidePort ? `${rule.outsidePort}–${end}` : String(rule.outsidePort);
}

/** The ports as typed in the field again ("27015-27030"). */
export function portsText(rule: Pick<PortForwardRule, 'outsidePort' | 'outsidePortEnd'>): string {
  return formatPorts(rule).replace('–', '-');
}

/** "TCP 8080 → 80" or "UDP 27015–27030": what a rule forwards, for the row under its name. */
export function ruleSummary(rule: PortForwardRule): string {
  const protocol = PROTOCOL_OPTIONS.find((option) => option.value === rule.protocol)?.label ?? rule.protocol;
  const inside = rule.insidePort && rule.insidePort !== rule.outsidePort ? ` → ${rule.insidePort}` : '';
  return `${protocol} ${formatPorts(rule)}${inside}`;
}

/** Whether two rules forward a port in common on the same protocol (as the router checks). */
export function portsOverlap(x: PortForwardRule, y: PortForwardRule): boolean {
  if (x.protocol !== 'both' && y.protocol !== 'both' && x.protocol !== y.protocol) return false;
  const xEnd = Math.max(x.outsidePort, x.outsidePortEnd ?? 0);
  const yEnd = Math.max(y.outsidePort, y.outsidePortEnd ?? 0);
  return x.outsidePort <= yEnd && y.outsidePort <= xEnd;
}

/** The problems the sheet can show before saving, by field key (the router's keys). */
export type RuleProblems = Partial<Record<'name' | 'outsidePort' | 'insidePort' | 'target', string>>;

/**
 * The checks the router makes on one rule (internal/agent/portforward.go), so the sheet can
 * show them as the owner types. `others` are the rest of the list, to refuse a port another
 * enabled rule already forwards. A device without a fixed address isn't a problem here: the
 * sheet gives it one first.
 */
export function ruleProblems(rule: PortForwardRule, others: PortForwardRule[]): RuleProblems {
  const problems: RuleProblems = {};
  const name = rule.name.trim();
  if (name === '' || [...name].length > 40) problems.name = 'Give the rule a name of up to 40 characters.';
  const end = Math.max(rule.outsidePort, rule.outsidePortEnd ?? 0);
  if (rule.protocol !== 'tcp' && rule.outsidePort <= 68 && end >= 67) {
    problems.outsidePort = 'RasBeacon needs UDP ports 67 and 68 to get its own address.';
  }
  const taken = rule.enabled ? others.find((other) => other.enabled && portsOverlap(rule, other)) : undefined;
  if (taken && !problems.outsidePort) problems.outsidePort = `"${taken.name}" already forwards this port.`;
  const inside = rule.insidePort ?? 0;
  if (inside !== 0 && end !== rule.outsidePort && inside !== rule.outsidePort) {
    problems.insidePort = 'A range keeps its ports: leave the inside port empty.';
  }
  if (!rule.mac && !rule.plugin) problems.target = 'Choose a device or an installed plugin.';
  return problems;
}
