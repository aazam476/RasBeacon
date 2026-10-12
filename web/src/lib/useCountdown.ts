import { useEffect, useState } from 'react';

/**
 * Counts `seconds` down once a second, for display only (a countdown on screen). It starts over
 * whenever `seconds` changes, for example when the router sends a fresh value, and stops at 0.
 */
export function useCountdown(seconds: number): number {
  const [left, setLeft] = useState(seconds);
  const [startedFrom, setStartedFrom] = useState(seconds);

  // A new value from the router replaces the local count.
  if (startedFrom !== seconds) {
    setStartedFrom(seconds);
    setLeft(seconds);
  }

  useEffect(() => {
    if (left <= 0) return;
    const timer = setTimeout(() => setLeft((current) => Math.max(0, current - 1)), 1000);
    return () => clearTimeout(timer);
  }, [left]);

  return left;
}
