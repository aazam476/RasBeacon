/** Joins class names and skips falsy values: `cn('a', isOn && 'b')` → `'a b'`. */
export function cn(...classes: Array<string | false | null | undefined>): string {
  return classes.filter(Boolean).join(' ');
}
