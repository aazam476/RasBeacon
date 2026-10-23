import { useState, type ReactNode } from 'react';
import { Section } from './Section';
import { Sheet } from './Sheet';
import { TextField } from './Field';

interface RenameSheetProps {
  /** E.g. "Rename device". */
  title: string;
  /** The field's label, e.g. "Name". */
  label: string;
  initial: string;
  placeholder?: string;
  maxLength?: number;
  /** Under the field: what the name is for, or what happens. */
  footer?: ReactNode;
  /** The field's name in the router's errors (default `name`). */
  errorKey?: string;
  /** While saving (default "Saving…"), e.g. "Restarting…" when saving restarts something. */
  busyLabel?: string;
  /**
   * Checks the name while it is typed: a message keeps Save gray and shows under the field, so
   * a rule (such as a host name's characters) is clear before saving.
   */
  check?: (name: string) => string | undefined;
  /** A name like a host name or a code: no capitals, corrections or suggestions while typing. */
  plain?: boolean;
  /** An empty name may be saved too: it removes the name (a device goes back to its own). */
  allowEmpty?: boolean;
  onSave: (name: string) => Promise<void>;
  onClose: () => void;
}

/**
 * A sheet with one name to type. Save stays gray while the name is blank, since Cancel is
 * there for leaving it as it was, unless `allowEmpty` says a blank name means something.
 */
export function RenameSheet({
  title,
  label,
  initial,
  placeholder,
  maxLength,
  footer,
  errorKey = 'name',
  busyLabel = 'Saving…',
  check,
  plain = false,
  allowEmpty = false,
  onSave,
  onClose,
}: RenameSheetProps) {
  const [name, setName] = useState(initial);
  const problem = name.trim() === '' ? undefined : check?.(name.trim());

  return (
    <Sheet
      title={title}
      confirmLabel="Save"
      busyLabel={busyLabel}
      confirmDisabled={(name.trim() === '' && !allowEmpty) || problem !== undefined}
      onConfirm={() => onSave(name.trim())}
      onClose={onClose}
    >
      {(errors) => (
        <Section level={3} footer={footer}>
          <TextField
            label={label}
            data-autofocus
            placeholder={placeholder}
            maxLength={maxLength}
            value={name}
            onChange={setName}
            error={problem ?? errors[errorKey]}
            autoComplete="off"
            {...(plain && { autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false })}
          />
        </Section>
      )}
    </Sheet>
  );
}
