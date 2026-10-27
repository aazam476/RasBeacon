/**
 * Reject an action with this to show messages next to the inputs they are about, as well as (or
 * instead of) one message for the whole form.
 *
 *   throw new FieldErrors({ hotspotSsid: 'Must be 1 to 32 bytes long.' });
 *
 * Each screen lists the keys it shows in the comment of the action it calls (see device.ts).
 * A key the screen doesn't know is not shown, so pass a `message` too when unsure.
 */
export class FieldErrors extends Error {
  readonly fields: Readonly<Record<string, string>>;

  constructor(fields: Record<string, string>, message = '') {
    super(message);
    this.name = 'FieldErrors';
    this.fields = fields;
  }
}

/**
 * What went wrong, in words, for a screen that shows one message: a field's message (a refused
 * setting's error has no message of its own), or the error's.
 */
export function errorText(problem: unknown): string {
  if (problem instanceof FieldErrors) return Object.values(problem.fields)[0] ?? problem.message;
  return problem instanceof Error ? problem.message : String(problem);
}
