import { createContext, useContext } from 'react';

/** Shows an error message at the bottom of the screen; `ErrorToastProvider` supplies it. */
export const ErrorToastContext = createContext<(message: string) => void>(() => {});

/** The function that shows an error message in the error toast (see `ErrorToastProvider`). */
export function useErrorToast(): (message: string) => void {
  return useContext(ErrorToastContext);
}
