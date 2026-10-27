import { ruleProblems } from '@/lib/portForward';
import { FieldErrors } from '../errors';
import type { PortForwardingSettings, PortForwardRule } from '../types';
import type { MockRouter } from './mockData';

/** Ports often attacked from the internet, as the router warns about them (internal/agent/portforward.go). */
const RISKY_PORTS: Record<number, string> = {
  22: 'SSH',
  23: 'Telnet',
  445: 'Windows file sharing',
  3389: 'Remote Desktop',
  5900: 'VNC',
};

/** What a private address on the uplink looks like (RFC 1918), and the carriers' shared one. */
function reachability(address: string): PortForwardingSettings['reachability'] {
  if (/^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(address)) return 'shared';
  if (/^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(address)) return 'private';
  return 'public';
}

/** The Port forwarding page as the router answers it: the rules with their targets filled in. */
export function portForwardingView(router: MockRouter): PortForwardingSettings {
  const { uplink, clients, vpn } = router.status;
  // While the VPN is up, it may route a public IPv6 network to RasBeacon (pinholes open there).
  const ipv6Prefix = vpn.state === 'up' && vpn.routedIpv6Prefix ? vpn.routedIpv6Prefix : undefined;
  const { plugins } = router.plugins;
  const rules = router.portForwardingRules.map((rule) => {
    const client = clients.find((c) => c.mac === rule.mac);
    const plugin = plugins.find((p) => p.id === rule.plugin);
    const risky = Object.keys(RISKY_PORTS)
      .map(Number)
      .find((port) => port >= rule.outsidePort && port <= Math.max(rule.outsidePort, rule.outsidePortEnd ?? 0));
    return {
      ...rule,
      target: plugin?.name ?? (client ? client.name || client.hostname || client.mac : (rule.mac ?? rule.plugin)),
      address: plugin ? '10.88.0.2' : client?.fixedAddress,
      // A made-up public address for the device, from the end of its MAC address.
      ipv6Addresses:
        ipv6Prefix && rule.ipv6 && client
          ? [`${ipv6Prefix.replace(/::\/\d+$/, '')}::${client.mac.slice(-5).replace(':', '')}`]
          : undefined,
      warning: risky
        ? `Port ${risky} (${RISKY_PORTS[risky]}) is often attacked from the internet. Use a strong password there, or another outside port.`
        : undefined,
    };
  });
  if (!uplink.connected) return { rules, reachability: 'offline', ipv6Prefix };
  return { rules, outsideAddress: uplink.address, reachability: reachability(uplink.address), ipv6Prefix };
}

/** Checks a new list as the router does, and answers it as saved (new rules get an ID). */
export function checkPortForwarding(router: MockRouter, rules: PortForwardRule[]): PortForwardRule[] {
  return rules.map((rule, index) => {
    if (!(rule.outsidePort >= 1 && rule.outsidePort <= 65535)) {
      throw new FieldErrors({ outsidePort: 'Use a port from 1 to 65535.' });
    }
    const problems = ruleProblems(rule, rules.slice(0, index));
    const [field] = Object.keys(problems) as (keyof typeof problems)[];
    if (field) throw new FieldErrors({ [field]: problems[field]! });
    const client = router.status.clients.find((c) => c.mac === rule.mac);
    if (rule.enabled && rule.mac && !client?.fixedAddress) {
      throw new FieldErrors({ target: 'Give this device a fixed address first, so the rule always finds it.' });
    }
    // Only what a PUT keeps: the read-only fields are the router's.
    const { target: _target, address: _address, warning: _warning, ipv6Addresses: _ipv6, ...kept } = rule;
    return { ...kept, id: rule.id || `pf-${Date.now().toString(36)}-${index}` };
  });
}
