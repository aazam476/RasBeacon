import { useEffect, useId, useRef, useSyncExternalStore } from 'react';

/**
 * One "Last refresh failed" note per page. When RasBeacon goes offline, every piece of data on
 * a page fails its refresh at once; each `Loaded` reports its failure here instead of showing a
 * note of its own, and the page's `StaleNotice` (in the layouts, above the page) shows a single
 * note whose "Try again" loads them all again. Without a `StaleNotice` on screen (a screen
 * rendered on its own, as in tests), each `Loaded` shows its own note as before.
 */

/** A piece of data whose last refresh failed: why, and how to load it again. */
export interface StaleEntry {
  error: string;
  retry: () => Promise<void>;
}

/** What the store hands to React: it changes as a whole, so React notices each change. */
interface Snapshot {
  /** The failures, in the order they were reported. */
  entries: StaleEntry[];
  /** Whether a `StaleNotice` is on screen to show them. */
  hosted: boolean;
}

// The store: the failures by the id of the Loaded that reported them, how many StaleNotices
// are on screen, the snapshot React reads, and who to tell about changes.
const entries = new Map<string, StaleEntry>();
let hosts = 0;
let snapshot: Snapshot = { entries: [], hosted: false };
const listeners = new Set<() => void>();

/** Makes a new snapshot (so React sees the change) and tells every subscriber. */
function changed() {
  snapshot = { entries: [...entries.values()], hosted: hosts > 0 };
  for (const listener of listeners) listener();
}

/** Lets a component hear about every change (useSyncExternalStore). */
function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The current snapshot (useSyncExternalStore). */
const getSnapshot = () => snapshot;

/**
 * Reports that this piece of data's last refresh failed (`error`), or that it didn't
 * (`undefined`). Returns whether the page's `StaleNotice` shows it, so the caller needn't.
 */
export function useStaleReport(error: string | undefined, retry: () => Promise<void>): boolean {
  const id = useId();
  // Callers pass a new function on every render; the latest one is used, without reporting
  // again (which would render again, forever).
  const latest = useRef(retry);
  useEffect(() => {
    latest.current = retry;
  });
  useEffect(() => {
    if (error === undefined) return;
    entries.set(id, { error, retry: () => latest.current() });
    changed();
    return () => {
      entries.delete(id);
      changed();
    };
  }, [id, error]);
  return useSyncExternalStore(subscribe, getSnapshot).hosted;
}

/** For the page's `StaleNotice`: the failures to show, while it is on screen. */
export function useStaleEntries(): StaleEntry[] {
  useEffect(() => {
    hosts += 1;
    changed();
    return () => {
      hosts -= 1;
      changed();
    };
  }, []);
  return useSyncExternalStore(subscribe, getSnapshot).entries;
}
