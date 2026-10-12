import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { Button } from './Button';
import { FormError, FormSuccess } from './FormMessage';

interface FormActionsProps {
  /** The submit button, e.g. "Save". */
  label: string;
  /** What it reads while saving, e.g. "Saving…". */
  busyLabel: string;
  pending: boolean;
  /** Why the last attempt failed. */
  error?: string | null;
  /** The confirmation after success, e.g. "Saved.". */
  success?: ReactNode;
  /** What pressing it will do, shown until there is an error or a confirmation. */
  note?: ReactNode;
  disabled?: boolean;
  /** Inside a row of a group (a disclosure's form), whose padding already lines the message up. */
  inRow?: boolean;
  className?: string;
}

/**
 * The end of a form: its message and the submit button, full width on phones (in reach of the
 * thumb) and at the right on computers, as in macOS.
 */
export function FormActions({
  label,
  busyLabel,
  pending,
  error,
  success,
  note,
  disabled,
  inRow = false,
  className,
}: FormActionsProps) {
  let message: ReactNode = null;
  if (error) message = <FormError>{error}</FormError>;
  else if (success) message = <FormSuccess>{success}</FormSuccess>;
  else if (note) message = <p className="text-footnote text-label-2">{note}</p>;

  return (
    <div className={cn('flex flex-col gap-3 lg:flex-row lg:items-center lg:gap-4', className)}>
      {message && <div className={cn('min-w-0 lg:flex-1', !inRow && 'px-4 lg:px-1')}>{message}</div>}
      <Button
        type="submit"
        size="lg"
        loading={pending}
        disabled={disabled}
        className="w-full lg:ml-auto lg:w-auto lg:min-w-24"
      >
        {pending ? busyLabel : label}
      </Button>
    </div>
  );
}
