// Small helpers that turn numbers from the API into words for the screen.

/** 3725 → "1 h 2 min"; 42 → "42 s". */
export function formatDuration(totalSeconds: number): string {
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = Math.floor(totalSeconds % 60);

  if (days > 0) return `${days} d ${hours} h`;
  if (hours > 0) return `${hours} h ${minutes} min`;
  if (minutes > 0) return `${minutes} min`;
  return `${seconds} s`;
}

/**
 * Wi-Fi signal strength in words. dBm values are negative; closer to zero
 * is stronger (-45 is excellent, -80 is poor).
 */
export function describeSignal(dbm: number): string {
  if (dbm === 0) return "unknown";
  if (dbm >= -55) return `excellent (${dbm} dBm)`;
  if (dbm >= -67) return `good (${dbm} dBm)`;
  if (dbm >= -75) return `fair (${dbm} dBm)`;
  return `poor (${dbm} dBm)`;
}
