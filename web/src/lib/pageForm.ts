import { createContext, useContext, useLayoutEffect, useState } from 'react';

/**
 * One Save for a whole page: each section keeps its own form (see useForm) and registers it here
 * as a part; the page's bar saves every part with changes, in the page's order, and throws them
 * all away with Discard. Used by the Internet page, whose sections each save to their own address
 * on the router.
 */

/** One section's form, as the page's bar sees it. */
export interface PagePart {
  /** Where the section is on the page: parts are saved from the top down. */
  order: number;
  /** True while there are edits that aren't saved yet. */
  dirty: boolean;
  /** True while it is being saved. */
  pending: boolean;
  /** Why it can't be saved as it is (shown at its field already), or undefined. */
  invalid?: boolean;
  /** The message of its last failed save that isn't about one of its fields. */
  error?: string | null;
  /** Saves the edits; resolves to true when the router took them. */
  submit: () => Promise<boolean>;
  /** Throws the edits away. */
  discard: () => void;
  /** What to say after it was saved, besides "Saved." (e.g. that networks are being joined). */
  savedNote?: string;
  /** The section's name, to point at it when a field there needs a fix. */
  title: string;
}

export interface Registry {
  set: (id: string, part: PagePart) => void;
  remove: (id: string) => void;
  parts: () => [string, PagePart][];
  /** Changes whenever a part's dirty, pending or error state changes, so the bar redraws. */
  version: number;
}

export const PageFormContext = createContext<Registry | null>(null);

/** Registers a section's form as part `id` of the page, for as long as the section is shown. */
export function usePagePart(id: string, part: PagePart) {
  const registry = useContext(PageFormContext);
  if (!registry) throw new Error('usePagePart needs a PageFormProvider around it');
  // After every render, so the bar always has the latest edits and functions.
  useLayoutEffect(() => registry.set(id, part));
  const { remove } = registry;
  useLayoutEffect(() => () => remove(id), [id, remove]);
}

/** The whole page's state and actions, for its Save bar. */
export function usePageForm() {
  const context = useContext(PageFormContext);
  if (!context) throw new Error('usePageForm needs a PageFormProvider around it');
  const registry: Registry = context;
  const [saving, setSaving] = useState(false);
  // What was last saved, to say so until the next edit.
  const [savedNotes, setSavedNotes] = useState<string[] | null>(null);
  // The section whose save failed last, while it still has edits.
  const [failedId, setFailedId] = useState<string | null>(null);

  const parts = registry.parts();
  const dirtyParts = parts.filter(([, part]) => part.dirty);
  const dirty = dirtyParts.length > 0;
  const failedPart = parts.find(([id, part]) => id === failedId && part.dirty)?.[1];
  // A message about the whole section, or else a pointer to the field that needs a fix.
  const failed = failedPart ? (failedPart.error ?? `Check ${failedPart.title}.`) : null;

  /** Saves every part with changes from the top down, and stops at the first that fails. */
  async function save() {
    setSaving(true);
    setSavedNotes(null);
    setFailedId(null);
    const notes: string[] = [];
    try {
      for (const [id, part] of registry.parts().filter(([, p]) => p.dirty)) {
        if (!(await part.submit())) {
          setFailedId(id);
          return;
        }
        if (part.savedNote) notes.push(part.savedNote);
      }
      setSavedNotes(notes);
    } finally {
      setSaving(false);
    }
  }

  function discard() {
    for (const [, part] of registry.parts()) part.discard();
    setSavedNotes(null);
    setFailedId(null);
  }

  return {
    /** True while any section has edits that aren't saved yet. */
    dirty,
    /** True while saving. */
    pending: saving || parts.some(([, part]) => part.pending),
    /** True when a section's edits can't be saved as they are (its field says why). */
    invalid: dirtyParts.some(([, part]) => part.invalid),
    /** Why the last save failed, while that section still has edits; null otherwise. */
    error: failed,
    /** After a save that worked: what to say (empty for a plain "Saved."); null otherwise. */
    savedNotes: dirty ? null : savedNotes,
    save,
    discard,
    /** Forgets the "Saved." message (the bar's close button). */
    dismissSaved: () => setSavedNotes(null),
  };
}
