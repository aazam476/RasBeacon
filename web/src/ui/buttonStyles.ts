import { cn } from '@/lib/cn';

export type ButtonVariant = 'primary' | 'secondary' | 'danger' | 'danger-filled' | 'plain' | 'plain-danger';
export type ButtonSize = 'lg' | 'md' | 'sm';

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  /** The main action of a screen or form: filled with the tint. Hover darkens, keeping the contrast. */
  primary:
    'bg-tint-fill font-semibold text-white hover:not-disabled:bg-[color-mix(in_srgb,var(--c-tint-fill)_88%,black)] disabled:bg-fill-2',
  /** Other actions: a gray capsule with tinted text. */
  secondary: 'bg-fill-2 font-semibold text-tint hover:not-disabled:bg-fill',
  /** Disruptive actions that are easy to undo (Disconnect, Remove): red text. */
  danger: 'bg-fill-2 font-semibold text-red hover:not-disabled:bg-fill',
  /** The final step of something that can't be undone (Erase everything, Reboot). */
  'danger-filled':
    'bg-red-strong font-semibold text-white hover:not-disabled:bg-[color-mix(in_srgb,var(--color-red-strong)_88%,black)] disabled:bg-fill-2',
  /** Inline actions in rows: tinted text only. */
  plain: 'font-semibold text-tint hover:not-disabled:text-tint/80',
  /** Inline destructive actions in rows: red text only. */
  'plain-danger': 'font-semibold text-red hover:not-disabled:text-red/80',
};

const SIZE_CLASSES: Record<ButtonSize, string> = {
  /** Form buttons and the main action of full pages: 50px tall on phones, 36px on computers. */
  lg: 'min-h-[3.125rem] gap-2 py-2 text-headline lg:min-h-9',
  /** Most buttons: 44px on phones, macOS's 32px on computers. */
  md: 'min-h-11 gap-1.5 py-2 text-body lg:min-h-8 lg:py-1',
  /** Compact actions in rows and bars. The ::before layer keeps the touch target 44px tall. */
  sm: 'relative min-h-[2.125rem] gap-1 py-1 text-subhead before:absolute before:inset-x-0 before:-inset-y-[5px] lg:min-h-7',
};

/** The sides of capsules; text-only buttons have none, so they line up with the text around them. */
const PADDING_CLASSES: Record<ButtonSize, string> = {
  lg: 'px-5 lg:px-4',
  md: 'px-4 lg:px-3.5',
  sm: 'px-3 lg:px-2.5',
};

/**
 * The look of a button, for `<Button>` and for links that should look like one:
 * `<Link to="/vpn" className={buttonClasses('secondary', 'sm')}>`.
 */
export function buttonClasses(variant: ButtonVariant = 'primary', size: ButtonSize = 'md'): string {
  const plain = variant === 'plain' || variant === 'plain-danger';
  return cn(
    // Capsules, as Apple's buttons are. Labels wrap rather than overflow when space is short.
    'pressable inline-flex max-w-full items-center justify-center rounded-full text-center',
    // Unavailable: gray, like iOS, rather than faded.
    'disabled:cursor-not-allowed disabled:text-label-3',
    VARIANT_CLASSES[variant],
    SIZE_CLASSES[size],
    !plain && PADDING_CLASSES[size],
  );
}
