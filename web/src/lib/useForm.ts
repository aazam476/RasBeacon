import { useState } from 'react';
import { useAction } from './useAction';

/**
 * A settings form. It shows `current` (the router's saved values) until the user edits a field,
 * then the edits. `submit()` runs `save` with the values; when that succeeds the edits are
 * dropped, so the form shows the router's values again, and `saved` stays true until the next
 * edit. A field's error message disappears once the user changes that field.
 *
 *   const form = useForm({ ...settings, password: '' }, saveHotspotSettings);
 *   <TextField value={form.values.ssid} onChange={(ssid) => form.setField('ssid', ssid)}
 *              error={form.fieldErrors.ssid} />
 */
export function useForm<T extends object>(current: T, save: (values: T) => Promise<unknown>) {
  const [edits, setEdits] = useState<T | null>(null);
  const [saved, setSaved] = useState(false);
  // Fields changed since the last submit, whose old error messages no longer apply.
  const [changed, setChanged] = useState<ReadonlySet<string>>(() => new Set());
  const action = useAction(save);
  const values = edits ?? current;

  /** Changes the values with a function, e.g. to reorder a list. `fields` names what changed. */
  function update(change: (values: T) => T, fields: string[] = []) {
    setEdits((edited) => change(edited ?? current));
    setSaved(false);
    if (fields.length > 0) setChanged((names) => new Set([...names, ...fields]));
  }

  function setField<K extends keyof T & string>(field: K, value: T[K]) {
    update((values) => ({ ...values, [field]: value }), [field]);
  }

  async function submit(): Promise<boolean> {
    setChanged(new Set());
    const succeeded = await action.run(values);
    if (succeeded) {
      setEdits(null);
      setSaved(true);
    }
    return succeeded;
  }

  /** Throws the edits away and shows the router's values again. */
  function discard() {
    setEdits(null);
    setChanged(new Set());
    action.clearErrors();
  }

  const fieldErrors: Record<string, string> = {};
  for (const [field, message] of Object.entries(action.fieldErrors)) {
    if (!changed.has(field)) fieldErrors[field] = message;
  }

  return {
    values,
    setField,
    update,
    /** True while there are edits that aren't saved. */
    dirty: edits !== null,
    submit,
    discard,
    pending: action.pending,
    /** The message for the whole form, if the last save failed. */
    error: action.error,
    fieldErrors,
    saved,
  };
}
