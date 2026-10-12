import type { Band, Security } from '@/data/types';

/** The hotspot's bands, as setup and the Hotspot page offer them. */
export const BAND_OPTIONS: { value: Band; label: string }[] = [
  { value: '2.4GHz', label: '2.4 GHz (works with every device, longer range)' },
  { value: '5GHz', label: '5 GHz (faster, shorter range)' },
];

export const HOTSPOT_SECURITY_OPTIONS: { value: Security; label: string }[] = [
  { value: 'wpa2-wpa3', label: 'WPA2/WPA3 (recommended)' },
  { value: 'wpa2', label: "WPA2 only (for old devices that can't join)" },
];
