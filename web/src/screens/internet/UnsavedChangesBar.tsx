import { useState } from 'react';
import { Button, FormError, FormSuccess, IconButton } from '@/ui';
import type { NetworkDraft } from './useNetworkDraft';

/**
 * Save and discard for the list of networks, floating at the bottom of the screen while there
 * are changes (above the tab bar on phones), so it is in reach wherever the change was made.
 * After saving it says so until dismissed.
 */
export function UnsavedChangesBar({ draft }: { draft: NetworkDraft }) {
  const [dismissed, setDismissed] = useState(false);
  // A new save shows its confirmation again.
  const [lastSaved, setLastSaved] = useState(draft.saved);
  if (draft.saved !== lastSaved) {
    setLastSaved(draft.saved);
    setDismissed(false);
  }

  const showSaved = draft.saved && !draft.dirty && !dismissed;
  if (!draft.dirty && !showSaved) return null;

  return (
    // Above the tab bar on phones, and above the demo's data switcher when it shows.
    <div className="glass sticky bottom-[calc(max(env(safe-area-inset-bottom),0.75rem)+4.75rem+var(--stress-toggle,0rem))] z-30 flex flex-col gap-3 rounded-[22px] p-3 pl-4 lg:bottom-[calc(1.5rem+var(--stress-toggle,0rem))] lg:flex-row lg:items-center lg:rounded-[14px]">
      {showSaved ? (
        <div className="flex flex-1 items-start justify-between gap-3">
          <FormSuccess className="py-1.5">
            Saved. RasBeacon is joining the networks now; see the Status page.
          </FormSuccess>
          <IconButton icon="close" ariaLabel="Dismiss" onClick={() => setDismissed(true)} />
        </div>
      ) : (
        <>
          <div className="flex-1">
            {draft.error ? (
              <FormError>{draft.error}</FormError>
            ) : (
              <p className="text-subhead text-label">You have changes that are not saved yet.</p>
            )}
          </div>
          <div className="flex flex-wrap gap-2 *:grow *:basis-0 *:min-w-fit lg:*:grow-0 lg:*:basis-auto">
            <Button variant="secondary" onClick={draft.discard} disabled={draft.pending}>
              Discard changes
            </Button>
            <Button loading={draft.pending} onClick={() => draft.save()}>
              {draft.pending ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
