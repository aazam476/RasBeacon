import { useCallback, useRef, useState } from 'react';
import { FieldErrors } from '@/data/errors';

const FALLBACK_ERROR = 'Something went wrong. Please try again.';
const NO_FIELD_ERRORS: Readonly<Record<string, string>> = {};

/**
 * Runs a device action from a button, switch or form. `pending` is true while it runs (for a
 * spinner), and `run()` resolves to whether it succeeded. When it fails, `error` holds the
 * message for the whole form and `fieldErrors` the messages for single inputs (when the action
 * rejected with `FieldErrors`). Arguments given to `run` are passed on to the action. Calls
 * made while the action is still running are ignored, so a double click or a second Enter
 * doesn't send the request twice. `onError` also receives the message, for controls that show
 * it elsewhere (the error toast).
 *
 *   const rescan = useAction(rescanEthernetPort);
 *   <Button loading={rescan.pending} onClick={() => rescan.run()}>Rescan Port</Button>
 */
export function useAction<Args extends unknown[]>(
  action: (...args: Args) => Promise<unknown>,
  onError?: (message: string) => void,
) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState(NO_FIELD_ERRORS);
  // A ref as well as state, because a second click can arrive before React re-renders.
  const running = useRef(false);

  const run = useCallback(
    async (...args: Args): Promise<boolean> => {
      if (running.current) return false;
      running.current = true;
      setPending(true);
      setError(null);
      setFieldErrors(NO_FIELD_ERRORS);
      try {
        await action(...args);
        return true;
      } catch (failure) {
        if (failure instanceof FieldErrors) {
          setFieldErrors(failure.fields);
          setError(failure.message || null);
          if (failure.message) onError?.(failure.message);
        } else {
          const message = failure instanceof Error && failure.message ? failure.message : FALLBACK_ERROR;
          setError(message);
          onError?.(message);
        }
        return false;
      } finally {
        running.current = false;
        setPending(false);
      }
    },
    [action, onError],
  );

  /** Hides the last failure's messages, e.g. when the user starts over. */
  const clearErrors = useCallback(() => {
    setError(null);
    setFieldErrors(NO_FIELD_ERRORS);
  }, []);

  return { pending, error, fieldErrors, run, clearErrors };
}
