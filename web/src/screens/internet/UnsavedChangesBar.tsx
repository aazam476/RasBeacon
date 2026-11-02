import { usePageForm } from '@/lib/pageForm';
import { Button, FormError, FormSuccess, IconButton } from '@/ui';

/**
 * The Internet page's one Save: it floats at the bottom of the screen while any section has
 * changes (above the tab bar on phones), so it is in reach wherever the change was made, and
 * saves them all. After saving it says so until dismissed or the next change.
 */
export function UnsavedChangesBar() {
  const page = usePageForm();
  if (!page.dirty && !page.savedNotes) return null;

  return (
    // Above the tab bar on phones, and above the demo's data switcher when it shows.
    <div
      role="region"
      aria-label="Unsaved changes"
      className="glass sticky bottom-[calc(max(env(safe-area-inset-bottom),0.75rem)+4.75rem+var(--stress-toggle,0rem))] z-30 flex flex-col gap-3 rounded-[22px] p-3 pl-4 lg:bottom-[calc(1.5rem+var(--stress-toggle,0rem))] lg:flex-row lg:items-center lg:rounded-[14px]"
    >
      {page.savedNotes ? (
        <div className="flex flex-1 items-start justify-between gap-3">
          <FormSuccess className="py-1.5">{['Saved.', ...page.savedNotes].join(' ')}</FormSuccess>
          <IconButton icon="close" ariaLabel="Dismiss" onClick={page.dismissSaved} />
        </div>
      ) : (
        <>
          <div className="flex-1">
            {page.error ? (
              <FormError>{page.error}</FormError>
            ) : (
              <p className="text-subhead text-label">You have changes that are not saved yet.</p>
            )}
          </div>
          <div className="flex flex-wrap gap-2 *:grow *:basis-0 *:min-w-fit lg:*:grow-0 lg:*:basis-auto">
            <Button variant="secondary" onClick={page.discard} disabled={page.pending}>
              Discard changes
            </Button>
            <Button loading={page.pending} disabled={page.invalid} onClick={() => void page.save()}>
              {page.pending ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
