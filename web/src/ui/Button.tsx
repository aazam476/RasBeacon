import type { ButtonHTMLAttributes } from 'react';
import { cn } from '@/lib/cn';
import { ActivityIndicator } from './ActivityIndicator';
import { buttonClasses, type ButtonSize, type ButtonVariant } from './buttonStyles';
import { Icon, type BaseIconName } from './Icon';

const ICON_SIZE: Record<ButtonSize, number> = { lg: 20, md: 18, sm: 16 };

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: BaseIconName;
  /** Shows the activity indicator and ignores presses until it is turned off. */
  loading?: boolean;
}

/**
 * A capsule button with an optional icon. It sizes to its label; stretch it with `w-full` or a
 * grid. For a button that runs a device action, use `ActionButton`, which handles `loading`.
 */
export function Button({
  variant = 'primary',
  size = 'md',
  icon,
  loading = false,
  type = 'button',
  className,
  onClick,
  children,
  ...props
}: ButtonProps) {
  const iconSize = ICON_SIZE[size];

  return (
    <button
      type={type}
      // While loading, the button keeps keyboard focus (a disabled button would drop it) but
      // ignores presses; cancelling the click also stops a submit button from submitting twice.
      aria-disabled={loading || undefined}
      aria-busy={loading || undefined}
      onClick={(event) => {
        if (loading) event.preventDefault();
        else onClick?.(event);
      }}
      className={cn(buttonClasses(variant, size), loading && 'cursor-wait', className)}
      {...props}
    >
      {/* The indicator takes the icon's place, or sits over the label, so the size never changes. */}
      {loading && icon ? (
        <ActivityIndicator size={iconSize} className="text-current" />
      ) : (
        icon && <Icon name={icon} size={iconSize} />
      )}
      {loading && !icon ? (
        <span className="grid place-items-center *:col-start-1 *:row-start-1">
          <span className="opacity-0">{children}</span>
          <ActivityIndicator size={iconSize} className="text-current" />
        </span>
      ) : (
        <span>{children}</span>
      )}
    </button>
  );
}
