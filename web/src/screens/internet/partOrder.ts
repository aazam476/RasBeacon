/**
 * The order the Internet page saves its sections in (top down, as they are on the page), for
 * its one Save (see lib/pageForm.tsx).
 */
export const PART_ORDER = {
  speeds: 1,
  networks: 2,
  modem: 3,
  mobileData: 4,
  mac: 5,
  dns: 6,
} as const;
