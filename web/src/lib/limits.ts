/**
 * Reading the limits typed into the device limit (Hotspot) and the data limit (Clients) fields,
 * so a mistake shows while typing. The router checks them again
 * (internal/config/config.go MaxDevicesLimit, MaxUsageLimitMB).
 */

/** The most devices on a network at once: the addresses DHCP hands out. */
export const MAX_DEVICES = 231;

/** The highest data limit, 1,000 GB (in MB, as the router keeps it). */
export const MAX_DATA_LIMIT_MB = 1_000_000;

/** What a limit field holds: a number to save, or why it can't be saved. */
export type LimitReading = { ok: true; value: number } | { ok: false; error: string };

/** The device limit as typed: empty means no limit (0). */
export function readDeviceLimit(text: string): LimitReading {
  const trimmed = text.trim();
  if (trimmed === '') return { ok: true, value: 0 };
  const count = /^\d+$/.test(trimmed) ? Number(trimmed) : NaN;
  if (!(count >= 1 && count <= MAX_DEVICES)) {
    return { ok: false, error: `Enter a number from 1 to ${MAX_DEVICES}, or leave it empty for no limit.` };
  }
  return { ok: true, value: count };
}

/** The units the data limit can be typed in. */
export type DataUnit = 'MB' | 'GB';

/** The data limit as typed, in MB: empty means no limit (0). "1.5" GB is 1,500 MB. */
export function readDataLimit(text: string, unit: DataUnit): LimitReading {
  const trimmed = text.trim().replace(',', '.');
  if (trimmed === '') return { ok: true, value: 0 };
  const amount = /^\d+(\.\d+)?$/.test(trimmed) ? Number(trimmed) : NaN;
  const megabytes = Math.round(amount * (unit === 'GB' ? 1000 : 1));
  if (!(megabytes >= 1 && megabytes <= MAX_DATA_LIMIT_MB)) {
    return { ok: false, error: 'Enter an amount up to 1,000 GB, or leave it empty for no limit.' };
  }
  return { ok: true, value: megabytes };
}

/** How a saved data limit is shown in the field: whole GB in GB, anything else in MB. */
export function showDataLimit(limitMb: number): { text: string; unit: DataUnit } {
  if (limitMb <= 0) return { text: '', unit: 'GB' };
  if (limitMb % 1000 === 0) return { text: String(limitMb / 1000), unit: 'GB' };
  return { text: String(limitMb), unit: 'MB' };
}
