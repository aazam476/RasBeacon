import { useEffect } from 'react';
import { useLocation } from 'react-router';

/**
 * A small "Demo" tag at the top right, linking to the page of demo states. It sits beside the
 * phone bar's log-out button and is part of the demo only. While it shows, `--demo-tag` holds
 * the room it takes, so a title at the top of a full page can stay clear of it.
 */
export function DemoTag() {
  const { pathname } = useLocation();
  const shown = pathname !== '/demo';

  useEffect(() => {
    if (!shown) return;
    const root = document.documentElement;
    root.style.setProperty('--demo-tag', '6.5rem');
    return () => {
      root.style.removeProperty('--demo-tag');
    };
  }, [shown]);

  if (!shown) return null;

  return (
    // A landmark of its own, so screen readers find it outside the page's regions.
    <aside aria-label="Demo">
      <a
        href="#/demo"
        title="Sample data. Open the list of demo states"
        className="pressable fixed top-[calc(env(safe-area-inset-top)+0.9375rem)] right-[3.75rem] z-50 rounded-full bg-[color-mix(in_srgb,var(--c-orange-fill)_14%,var(--c-cell))] px-2.5 py-0.5 text-footnote font-semibold text-orange no-underline lg:top-3 lg:right-6"
      >
        Demo
      </a>
    </aside>
  );
}
