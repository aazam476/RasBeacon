import type { ReactNode } from 'react';
import type { Loadable } from '@/data/device';
import { ActionButton, ActivityIndicator, Notice } from '@/ui';

interface LoadedProps<T> {
  data: Loadable<T>;
  /** What is being loaded, for the messages: "the status", "the hotspot settings". */
  what: string;
  /** Loads it again ("Try again"). */
  onRetry: () => Promise<void>;
  children: (data: T) => ReactNode;
}

/**
 * Shows `children` with the data once it has loaded. Until then it shows that the data is
 * loading, or why it couldn't be loaded with a "Try again" button. When a later refresh fails,
 * the older data stays and a note above it says so.
 */
export function Loaded<T>({ data, what, onRetry, children }: LoadedProps<T>) {
  const retry = (
    <ActionButton variant="secondary" size="sm" icon="refresh" action={onRetry}>
      Try again
    </ActionButton>
  );

  if (data.data === undefined) {
    if (data.error) {
      return (
        <Notice tone="danger" role="alert" title={`Can't load ${what}`} actions={retry}>
          <p>{data.error}</p>
        </Notice>
      );
    }
    return (
      <div className="grid min-h-40 place-items-center">
        <ActivityIndicator size={24} label={`Loading ${what}…`} className="text-subhead" />
      </div>
    );
  }

  return (
    <>
      {data.error && (
        <Notice tone="warning" role="alert" actions={retry}>
          <p>Last refresh failed: {data.error}</p>
        </Notice>
      )}
      {children(data.data)}
    </>
  );
}
