// useLoad: the one place where screens load data from the API.
//
// React's useEffect is the part of React that most often confuses people,
// so it appears only here. Every screen that needs data calls useLoad with
// a function that fetches it, and gets back { data, error, loading, reload }.
//
// Example:
//   const { data, error, loading, reload } = useLoad(getStatus);

import { useCallback, useEffect, useState } from "react";

export interface LoadResult<T> {
  /** The loaded data, or undefined until the first load finishes. */
  data: T | undefined;
  /** An error message if the last load failed, otherwise null. */
  error: string | null;
  /** True while a load is in progress. */
  loading: boolean;
  /** Loads the data again (for example after saving changes). */
  reload: () => void;
}

/**
 * Calls `load` when the screen opens and whenever `reload` is called.
 * If `refreshEverySeconds` is given, it also reloads on that interval
 * (used by the status page to keep the client list current).
 *
 * `load` should be a function defined outside the component (like getStatus
 * from api/client.ts), so it is the same function on every render.
 */
export function useLoad<T>(load: () => Promise<T>, refreshEverySeconds?: number): LoadResult<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  // Changing this number tells the effect below to run again.
  const [reloadCounter, setReloadCounter] = useState(0);

  const reload = useCallback(() => setReloadCounter((n) => n + 1), []);

  useEffect(() => {
    // If the screen closes before the request finishes, ignore the answer.
    let screenStillOpen = true;
    setLoading(true);
    load()
      .then((result) => {
        if (screenStillOpen) {
          setData(result);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (screenStillOpen) {
          setError(err instanceof Error ? err.message : String(err));
        }
      })
      .finally(() => {
        if (screenStillOpen) setLoading(false);
      });
    return () => {
      screenStillOpen = false;
    };
  }, [load, reloadCounter]);

  useEffect(() => {
    if (!refreshEverySeconds) return;
    const timer = setInterval(reload, refreshEverySeconds * 1000);
    return () => clearInterval(timer);
  }, [refreshEverySeconds, reload]);

  return { data, error, loading, reload };
}
