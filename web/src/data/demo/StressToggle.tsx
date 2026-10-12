import { useEffect } from 'react';
import { useLocation } from 'react-router';
import { cn } from '@/lib/cn';

/**
 * The stress-test data sets, one tap apart: the sample router, then the worst case the router
 * can send, nothing at all, and exactly one of everything. `sample-data` is no scenario, so it
 * applies nothing; it only keeps this switcher on screen.
 */
const DATA_SETS = [
  { id: 'sample-data', label: 'Demo data' },
  { id: 'worst-case', label: 'Worst case' },
  { id: 'no-clients', label: 'Empty' },
  { id: 'one-of-each', label: 'One' },
] as const;

/**
 * A plain switcher at the bottom of the screen while one of the stress-test data sets is open
 * (from "Stress test" on `/#/demo`), to flip the page you are on between them. It reloads the
 * app with the other data set and keeps the page. Part of the demo only: it is not part of the
 * design under test, so it stays small and neutral. While it shows, `--stress-toggle` holds the
 * room it takes, so the bar of unsaved changes floats above it instead of under it.
 */
export function StressToggle() {
  const { pathname } = useLocation();
  const current = new URLSearchParams(window.location.search).get('scenario');
  const shown = pathname !== '/demo' && DATA_SETS.some((set) => set.id === current);

  useEffect(() => {
    if (!shown) return;
    const root = document.documentElement;
    root.style.setProperty('--stress-toggle', '2.75rem');
    return () => {
      root.style.removeProperty('--stress-toggle');
    };
  }, [shown]);

  if (!shown) return null;

  return (
    <aside
      aria-label="Stress test data"
      className="pointer-events-none fixed inset-x-0 bottom-[calc(max(env(safe-area-inset-bottom),0.75rem)+4.75rem)] z-40 flex justify-center px-3 lg:bottom-4"
    >
      <div className="glass pointer-events-auto flex flex-wrap justify-center gap-0.5 rounded-[12px] p-1">
        {DATA_SETS.map((set) => (
          <a
            key={set.id}
            href={`${window.location.pathname}?scenario=${set.id}#${pathname}`}
            aria-current={set.id === current ? 'page' : undefined}
            className={cn(
              'rounded-[8px] px-2 py-1 text-footnote font-semibold whitespace-nowrap text-label no-underline',
              set.id === current ? 'bg-thumb shadow-thumb' : 'hover:bg-fill-2',
            )}
          >
            {set.label}
          </a>
        ))}
      </div>
    </aside>
  );
}
