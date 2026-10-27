import type { Band, Security, UplinkStandard, WiFiStandard } from '@/data/types';

const BAND_OPTIONS: { value: Band; label: string }[] = [
  { value: '2.4GHz', label: '2.4 GHz (works with every device, longer range)' },
  { value: '5GHz', label: '5 GHz (faster, shorter range)' },
  { value: '6GHz', label: '6 GHz (fastest, Wi-Fi 6E devices only)' },
];

/**
 * The hotspot's bands, as setup and the Hotspot page offer them. `no5GHz` is the router saying
 * the hotspot's radio has no 5 GHz band (like the Pi Zero 2 W's), so only 2.4 GHz is offered;
 * `offer6GHz` that it can run a 6 GHz hotspot where the country allows one.
 */
export function bandOptions(no5GHz: boolean | undefined, offer6GHz = false): { value: Band; label: string }[] {
  return BAND_OPTIONS.filter(
    (option) =>
      option.value === '2.4GHz' || (option.value === '5GHz' && !no5GHz) || (option.value === '6GHz' && offer6GHz),
  );
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
export function securityOptions(noWpa3: boolean | undefined, sixGHz = false): { value: Security; label: string }[] {
  if (sixGHz) return SECURITY_OPTIONS.filter((option) => option.value === 'wpa3'); // 6 GHz is WPA3 only
  return noWpa3 ? SECURITY_OPTIONS.filter((option) => option.value !== 'wpa3') : SECURITY_OPTIONS;
}

/** The best standard a radio runs on a band (what the router picks when none was chosen). */
export function bestStandard(band: Band, noWifi6: boolean | undefined): WiFiStandard {
  if (!noWifi6 || band === '6GHz') return 'wifi6';
  return band === '5GHz' ? 'wifi5' : 'wifi4';
}

/**
 * The Wi-Fi standards the Hotspot page offers: only what the adapter can run on the band, the best
 * marked "recommended".
 */
export function standardOptions(band: Band, noWifi6: boolean | undefined): { value: WiFiStandard; label: string }[] {
  const best = bestStandard(band, noWifi6);
  if (band === '6GHz') return [{ value: 'wifi6', label: 'Wi-Fi 6E' }];
  const options: { value: WiFiStandard; label: string }[] = [];
  if (!noWifi6) options.push({ value: 'wifi6', label: 'Wi-Fi 6' });
  if (band === '5GHz') options.push({ value: 'wifi5', label: 'Wi-Fi 5' });
  options.push({ value: 'wifi4', label: 'Wi-Fi 4, for very old devices' });
  return options.map((option) =>
    option.value === best ? { ...option, label: `${option.label} (recommended)` } : option,
  );
}

const STANDARD_LABELS: Record<UplinkStandard, string> = {
  wifi7: 'Wi-Fi 7',
  wifi6e: 'Wi-Fi 6E',
  wifi6: 'Wi-Fi 6',
  wifi5: 'Wi-Fi 5',
  wifi4: 'Wi-Fi 4',
};

/** "Wi-Fi 6E" for a connection's or a nearby network's standard, kept on one line. */
export function standardLabel(standard: UplinkStandard): string {
  return STANDARD_LABELS[standard].replace(' ', '\u00a0');
}
