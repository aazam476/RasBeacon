// Priority devices: the limits the router keeps (internal/config QoSConfig), and the speeds as
// people type them (Mb/s) and as the router keeps them (kbit/s).

/** How many devices can be priority devices. */
export const MAX_PRIORITY_DEVICES = 16;

/** The speeds the router accepts, in kbit/s: 1 Mb/s to 10 Gb/s (0 is "not set"). */
export const MIN_SPEED_KBPS = 1_000;
export const MAX_SPEED_KBPS = 10_000_000;

/** What is wrong with a speed in kbit/s, as the router says it, or undefined. */
export function speedProblem(kbps: number): string | undefined {
  if (kbps === 0 || (Number.isInteger(kbps) && kbps >= MIN_SPEED_KBPS && kbps <= MAX_SPEED_KBPS)) return undefined;
  return 'Use a speed from 1 Mb/s to 10,000 Mb/s, or leave it empty.';
}

/**
 * Reads a speed typed in Mb/s ("50", "12.5", "" for not set) as kbit/s. NaN when it isn't a
 * number, so speedProblem finds it.
 */
export function kbpsFromText(text: string): number {
  const trimmed = text.trim().replace(',', '.');
  if (trimmed === '') return 0;
  if (!/^\d+(\.\d+)?$/.test(trimmed)) return Number.NaN;
  return Math.round(Number(trimmed) * 1000);
}

/** A speed in kbit/s as it is typed: Mb/s without trailing zeros ("" for not set). */
export function textFromKbps(kbps: number): string {
  return kbps === 0 ? '' : String(Math.round(kbps / 100) / 10);
}
