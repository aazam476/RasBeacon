import type { ButtonHTMLAttributes } from 'react';
import { cn } from '@/lib/cn';
import { ActivityIndicator } from './ActivityIndicator';
import { Icon, type BaseIconName } from './Icon';

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: BaseIconName;
  /** Name read by screen readers, also shown as a tooltip. */
  ariaLabel: string;
  /** `glass`: a floating circle (bars); `plain`: just the symbol (inside rows and fields). */
  look?: 'glass' | 'plain';
  /** Shows the activity indicator instead of the symbol and ignores presses. */
  loading?: boolean;
}

/** A round, icon-only button. Its touch target is 44px whatever its size. */
export function IconButton({
  icon,
  ariaLabel,
  look = 'plain',
  loading = false,
  type = 'button',
  className,
  onClick,
  ...props
}: IconButtonProps) {
  return (
    <button
      type={type}
      aria-label={ariaLabel}
      title={ariaLabel}
      aria-busy={loading || undefined}
      onClick={(event) => {
        if (!loading) onClick?.(event);
      }}
      className={cn(
        'pressable relative inline-flex shrink-0 items-center justify-center rounded-full disabled:opacity-40',
        'before:absolute before:-inset-1.5',
        look === 'glass' ? 'glass size-9 text-label' : 'size-8 text-label-2 hover:text-label',
        loading && 'cursor-wait',
        className,
      )}
      {...props}
    >
      {loading ? <ActivityIndicator size={18} className="text-current" /> : <Icon name={icon} size={20} />}
    </button>
  );
}
