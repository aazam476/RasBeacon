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
  onSave: (name: string) => Promise<void>;
  onClose: () => void;
}

/**
 * A sheet with one name to type. Save stays gray while the name is blank, since Cancel is
 * there for leaving it as it was.
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
  onSave,
  onClose,
}: RenameSheetProps) {
  const [name, setName] = useState(initial);

  return (
    <Sheet
      title={title}
      confirmLabel="Save"
      busyLabel={busyLabel}
      confirmDisabled={name.trim() === ''}
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
            error={errors[errorKey]}
            autoComplete="off"
          />
        </Section>
      )}
    </Sheet>
  );
}
