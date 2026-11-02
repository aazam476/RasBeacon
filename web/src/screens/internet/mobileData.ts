import type { MobileData } from '@/data/types';

/** Before RasBeacon counted anything (or with an older one): nothing used, no limit. */
export const NO_MOBILE_DATA: MobileData = { usedBytes: 0, limitMb: 0, resetDay: 1 };

/** "October 1" (in the browser's language and order). */
export function sinceLabel(since: string | undefined): string {
  const date = since ? new Date(since) : null;
  if (!date || Number.isNaN(date.getTime())) return 'the start of the month';
  return date.toLocaleDateString(undefined, { day: 'numeric', month: 'long', timeZone: 'UTC' });
}
