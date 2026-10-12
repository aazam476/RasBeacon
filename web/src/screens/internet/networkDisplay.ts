import type { EAPMethod, NearbyNetwork, UplinkSecurity } from '@/data/types';

/** The three kinds of network RasBeacon can join, as the "Security" choice names them. */
export const SECURITY_OPTIONS: { value: UplinkSecurity; label: string }[] = [
  { value: 'personal', label: 'Password (WPA2/WPA3-Personal)' },
  { value: 'enterprise', label: 'Username and password (WPA2/WPA3-Enterprise, e.g. eduroam)' },
  { value: 'open', label: 'Open (no password)' },
];

/** Short names for the saved networks list. */
export const SECURITY_NAMES: Record<UplinkSecurity, string> = {
  open: 'Open',
  personal: 'Password',
  enterprise: 'Enterprise',
};

/** Enterprise login methods: exactly the three the router supports. */
export const METHOD_OPTIONS: { value: EAPMethod; label: string }[] = [
  { value: 'ttls-pap', label: 'TTLS (PAP)' },
  { value: 'peap-mschapv2', label: 'PEAP (MSCHAPv2)' },
  { value: 'tls', label: 'TLS (certificate)' },
];

export function methodName(method: EAPMethod): string {
  return METHOD_OPTIONS.find((option) => option.value === method)?.label ?? method;
}

/** Whether RasBeacon can join a network found by a scan ("other" means WEP and the like). */
export function canJoin(network: NearbyNetwork): network is NearbyNetwork & { security: UplinkSecurity } {
  return network.security !== 'other';
}
