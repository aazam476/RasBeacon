import { useSyncExternalStore } from 'react';

/** The desktop layout starts at Tailwind's `lg` breakpoint (64rem = 1024px). */
export const DESKTOP_QUERY = '(min-width: 64rem)';

/** True while the CSS media query matches; updates when the window is resized. */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const mediaQuery = window.matchMedia(query);
      mediaQuery.addEventListener('change', onChange);
      return () => mediaQuery.removeEventListener('change', onChange);
    },
    () => window.matchMedia(query).matches,
  );
}
