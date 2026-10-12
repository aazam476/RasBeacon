import { useAction } from '@/lib/useAction';
import { Button, type ButtonProps } from './Button';
import { useErrorToast } from './errorToastContext';

interface ActionButtonProps extends Omit<ButtonProps, 'onClick' | 'loading'> {
  /** The device action, e.g. `runSpeedtest`. */
  action: () => Promise<void>;
  /** Label while the action runs, e.g. "Running…"; by default the label stays the same. */
  busyLabel?: string;
}

/**
 * A button that runs a device action: it shows the activity indicator until the router answers
 * and ignores presses meanwhile. A failure appears in the error toast.
 */
export function ActionButton({ action, busyLabel, children, ...buttonProps }: ActionButtonProps) {
  const { pending, run } = useAction(action, useErrorToast());

  return (
    <Button loading={pending} onClick={() => run()} {...buttonProps}>
      {pending && busyLabel ? busyLabel : children}
    </Button>
  );
}
