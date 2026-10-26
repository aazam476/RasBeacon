import type { Band, Security } from '@/data/types';

const BAND_OPTIONS: { value: Band; label: string }[] = [
  { value: '2.4GHz', label: '2.4 GHz (works with every device, longer range)' },
  { value: '5GHz', label: '5 GHz (faster, shorter range)' },
];

/**
 * The hotspot's bands, as setup and the Hotspot page offer them. `no5GHz` is the router saying
 * the hotspot's radio has no 5 GHz band (like the Pi Zero 2 W's), so only 2.4 GHz is offered.
 */
export function bandOptions(no5GHz: boolean | undefined): { value: Band; label: string }[] {
  return no5GHz ? BAND_OPTIONS.filter((option) => option.value === '2.4GHz') : BAND_OPTIONS;
}

const SECURITY_OPTIONS: { value: Security; label: string }[] = [
  { value: 'wpa2-wpa3', label: 'WPA2/WPA3 (recommended)' },
  { value: 'wpa3', label: 'WPA3 only (safest; newer devices only)' },
  { value: 'wpa2', label: "WPA2 only (for old devices that can't join)" },
];

/**
 * The hotspot's security choices. `noWpa3` is the router saying the hotspot's radio can't run a
 * WPA3-only hotspot (like the Pi's built-in Wi-Fi), so "WPA3 only" isn't offered.
 */
export function securityOptions(noWpa3: boolean | undefined): { value: Security; label: string }[] {
  return noWpa3 ? SECURITY_OPTIONS.filter((option) => option.value !== 'wpa3') : SECURITY_OPTIONS;
}
