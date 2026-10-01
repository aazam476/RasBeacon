// useLongPoll: keeps asking the API for the next update while `active` is
// true. Each request waits on the device until something changed (a "long
// poll"), so updates show up at once without asking many times a second.
//
// Like useLoad, this is one of the few places that use React's useEffect.
//
// Example (the zero-leak login's pictures):
//   const frames = useLongPoll(browsing, nextFrame, (f) => !f.running);

import { useEffect, useState } from "react";

export interface LongPollResult<T> {
  /** The latest update, or undefined before the first one. */
  data: T | undefined;
  /** The last request's error, or null. */
  error: string | null;
}

/**
 * While `active`, calls `next(previous)` again and again, each time with the
 * previous answer. Stops when `isLast(answer)` is true or `active` becomes
 * false. After an error it waits 2 seconds before trying again.
 *
 * `next` and `isLast` should be defined outside the component so they are
 * the same functions on every render.
 */
export function useLongPoll<T>(
  active: boolean,
  next: (previous: T | undefined) => Promise<T>,
  isLast: (answer: T) => boolean,
): LongPollResult<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!active) return;
    let screenStillOpen = true;
    async function loop() {
      let previous: T | undefined = undefined;
      while (screenStillOpen) {
        try {
          const answer: T = await next(previous);
          if (!screenStillOpen) return;
          setData(answer);
          setError(null);
          if (isLast(answer)) return;
          previous = answer;
        } catch (err) {
          if (!screenStillOpen) return;
          setError(err instanceof Error ? err.message : String(err));
          await new Promise((resolve) => setTimeout(resolve, 2000));
        }
      }
    }
    void loop();
    return () => {
      screenStillOpen = false;
    };
  }, [active, next, isLast]);

  return { data, error };
}
