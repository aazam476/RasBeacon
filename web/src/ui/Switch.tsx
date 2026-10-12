import { useAction } from '@/lib/useAction';
import { useErrorToast } from './errorToastContext';

interface SwitchProps {
  checked: boolean;
  /** The device action that changes the setting. The switch moves once the router has done it. */
  onChange: (checked: boolean) => Promise<void>;
  /** Its name, when no visible text names it ("Kill switch"). */
  ariaLabel?: string;
  /** Or the id of the visible text that names it, and of the text that describes it. */
  labelledBy?: string;
  describedBy?: string;
  /** Greys it out, e.g. "Use the VPN" before any profile exists. */
  disabled?: boolean;
}

/**
 * An on/off switch that applies at once. While the router works it dims and ignores presses;
 * a failure appears in the error toast. For a switch saved with a form, use `FormSwitch`.
 */
export function Switch({ checked, onChange, ariaLabel, labelledBy, describedBy, disabled = false }: SwitchProps) {
  const { pending, run } = useAction(onChange, useErrorToast());

  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      aria-busy={pending || undefined}
      disabled={disabled}
      onClick={() => {
        if (!pending) run(!checked);
      }}
      className="switch"
    >
      <span className="switch-knob" />
    </button>
  );
}

interface FormSwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  labelledBy: string;
  describedBy?: string;
  disabled?: boolean;
}

/**
 * The same switch as part of a form, saved with it. It is a real checkbox, so it works with the
 * keyboard, screen readers and `getByLabel(…).check()` like one.
 */
export function FormSwitch({ checked, onChange, labelledBy, describedBy, disabled = false }: FormSwitchProps) {
  return (
    <span className="switch">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        // Transparent but covering the switch, so a press anywhere on it lands on the checkbox.
        className="absolute inset-0 z-10 size-full cursor-pointer appearance-none rounded-full opacity-0 disabled:cursor-not-allowed"
      />
      <span className="switch-knob" />
    </span>
  );
}
