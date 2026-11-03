import { ActionButton, Notice } from '@/ui';
import { useStaleEntries } from './staleNotices';

/**
 * The page's one "Last refresh failed" note, above the page (see staleNotices.ts), with a "Try
 * again" that loads again everything that failed. Nothing while every refresh works.
 */
export function StaleNotice() {
  const entries = useStaleEntries();
  if (entries.length === 0) return null;
  return (
    <Notice
      tone="warning"
      role="alert"
      actions={
        <ActionButton
          variant="secondary"
          size="sm"
          icon="refresh"
          action={async () => {
            await Promise.all(entries.map((entry) => entry.retry()));
          }}
        >
          Try again
        </ActionButton>
      }
    >
      <p>Last refresh failed: {entries[0].error}</p>
    </Notice>
  );
}
