import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { Icon } from './Icon';

/** Why a field's value or a form was refused: red text after a symbol, announced at once. */
export function FormError({ id, className, children }: { id?: string; className?: string; children: ReactNode }) {
  return (
    <p id={id} role="alert" className={cn('flex items-start gap-1.5 text-subhead text-red', className)}>
      <Icon name="error" filled size={16} className="mt-0.5 lg:mt-px" />
      <span className="min-w-0 wrap-break-word">{children}</span>
    </p>
  );
}

/** A confirmation after an action, such as "Saved.": green text after a check mark. */
export function FormSuccess({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <p role="status" className={cn('flex items-start gap-1.5 text-subhead text-green', className)}>
      <Icon name="check_circle" filled size={16} className="mt-0.5 lg:mt-px" />
      <span className="min-w-0 wrap-break-word">{children}</span>
    </p>
  );
}
