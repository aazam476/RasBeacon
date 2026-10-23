import type { Client } from '@/data/types';

interface SignalLevel {
  /** Lit bars on a four-bar meter. */
  bars: 1 | 2 | 3 | 4;
  /** Plain-language quality, shown next to dBm readings. */
  quality: 'Excellent' | 'Good' | 'Fair' | 'Weak';
}

/** Wi-Fi signal strength in plain terms. Bars and quality always agree. */
export function signalLevel(dbm: number): SignalLevel {
  if (dbm >= -65) return { bars: 4, quality: 'Excellent' };
  if (dbm >= -75) return { bars: 3, quality: 'Good' };
  if (dbm >= -85) return { bars: 2, quality: 'Fair' };
  return { bars: 1, quality: 'Weak' };
}

/**
 * No-break space. Put it between a number and its unit ("48.8 Mbps") or an arrow and its
 * number ("↓ 32.4") so the pair stays on one line when a narrow tile wraps the text.
 * In JSX text, write `&nbsp;` instead.
 */
export const NBSP = ' ';

/** Keeps a short reading from the router, such as "5 GHz" or "1 Gbps", on one line. */
export function keepTogether(text: string): string {
  return text.replaceAll(' ', NBSP);
}

/*
 * Readings arrive as plain numbers; these helpers decide how they are shown. Rates get one
 * decimal below 100 Mbps ("48.8") and whole numbers above ("942"); signal, percentages,
 * temperatures, memory and ping are whole numbers.
 */

/** The number of a data rate, without its unit: "48.8", "942". */
export function mbpsValue(mbps: number): string {
  return mbps >= 100 ? Math.round(mbps).toString() : mbps.toFixed(1);
}

/** "48.8 Mbps" */
export function formatMbps(mbps: number): string {
  return `${mbpsValue(mbps)}${NBSP}Mbps`;
}

/** "-54 dBm" */
export function formatDbm(dbm: number): string {
  return `${Math.round(dbm)}${NBSP}dBm`;
}

/** "-54 dBm (Excellent)" */
export function formatSignal(dbm: number): string {
  return `${formatDbm(dbm)} (${signalLevel(dbm).quality})`;
}

/** "↓ 32.4 ↑ 2.1 Mbps": a client's live download and upload rates. */
export function formatClientRate(client: Client): string {
  return `↓${NBSP}${mbpsValue(client.downMbps)} ↑${NBSP}${formatMbps(client.upMbps)}`;
}

/** `part` as a percentage of `whole`; 0 when there is no whole (e.g. swap turned off). */
export function percentOf(part: number, whole: number): number {
  return whole > 0 ? (part / whole) * 100 : 0;
}

/** A length of time, shortest unit last: "42s", "22m", "4h 12m", "14d 06h 42m". */
export function formatDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3_600);
  const minutes = Math.floor((seconds % 3_600) / 60);
  const pad = (value: number) => value.toString().padStart(2, '0');

  if (days > 0) return `${days}d ${pad(hours)}h ${pad(minutes)}m`;
  if (hours > 0) return `${hours}h ${pad(minutes)}m`;
  if (minutes > 0) return `${minutes}m`;
  return `${seconds}s`;
}

/** "14s ago", "2m ago". */
export function formatAgo(seconds: number): string {
  return seconds < 5 ? 'just now' : `${formatDuration(seconds)} ago`;
}

/** An RFC 3339 time as the viewer's own clock time: "08:12:03" (or "8:12:03 AM", by locale). */
export function formatClockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

/** Time left on a countdown as a clock: "8:32", "0:05". */
export function formatCountdown(totalSeconds: number): string {
  const seconds = Math.max(0, Math.ceil(totalSeconds));
  return `${Math.floor(seconds / 60)}:${(seconds % 60).toString().padStart(2, '0')}`;
}

/** "148.6 MB", "3.4 GB", "980 KB". Decimal units, as network tools show traffic. */
export function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit += 1;
  }
  const shown = unit === 0 || value >= 100 ? Math.round(value).toString() : value.toFixed(1);
  return `${shown}${NBSP}${units[unit]}`;
}

/** "5GHz" (as the router sends it) → "5 GHz". */
export function formatBand(band: string): string {
  return keepTogether(band.replace(/GHz$/, ' GHz'));
}

/** 2437 → "2.4 GHz"; 5180 → "5 GHz". */
export function bandOfFrequency(frequencyMhz: number): string {
  if (frequencyMhz < 3000) return `2.4${NBSP}GHz`;
  if (frequencyMhz < 5925) return `5${NBSP}GHz`;
  return `6${NBSP}GHz`;
}
