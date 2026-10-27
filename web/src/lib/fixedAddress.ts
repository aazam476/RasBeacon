import type { Client } from '@/data/types';

/**
 * What's wrong with an address typed for "Always use this address", or null when it can be used.
 * The same rules as the router's (internal/agent/fixedaddress.go): an IPv4 address on the
 * hotspot's network (a /24 around `lanAddress`, RasBeacon's own address there), not RasBeacon's
 * own, and not another device's. An empty box is fine: it means "no fixed address".
 */
export function fixedAddressProblem(
  text: string,
  mac: string,
  clients: readonly Client[],
  lanAddress = '10.87.0.1',
): string | null {
  const address = text.trim();
  if (address === '') return null;
  const network = lanAddress.split('.').slice(0, 3).join('.');
  const parts = address.split('.');
  const isIPv4 =
    parts.length === 4 &&
    parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255 && String(Number(part)) === part);
  if (!isIPv4) return `Enter an address like ${network}.2.`;
  const last = Number(parts[3]);
  if (parts.slice(0, 3).join('.') !== network || last === 0 || last === 255) {
    return `Use an address from ${network}.1 to ${network}.254.`;
  }
  if (address === lanAddress) return "That is RasBeacon's own address.";
  const other = clients.find((client) => client.mac !== mac && client.fixedAddress === address);
  if (other) return `${other.name || other.hostname || other.mac} already uses this address.`;
  return null;
}
