import { useId, useState, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from 'react';
import { cn } from '@/lib/cn';
import { FormError } from './FormMessage';
import { Icon } from './Icon';
import { IconButton } from './IconButton';

/** What a field's control needs to be connected to its label, help text and error. */
export interface FieldControlProps {
  id: string;
  'aria-describedby'?: string;
  'aria-invalid'?: true;
}

export interface FieldTextProps {
  label: string;
  /**
   * More of the label in the secondary color, e.g. "(leave empty to keep the current one)".
   * Screen readers read it as part of the label.
   */
  labelNote?: string;
  /** Help text under the control. */
  hint?: ReactNode;
  /** Why the value was refused, under the control. */
  error?: string;
}

interface FieldRowProps extends FieldTextProps {
  /** Keeps the label above the control on computers too, for wide controls (text boxes). */
  stacked?: boolean;
  children: (control: FieldControlProps) => ReactNode;
}

/**
 * One field as a row of a `Section`: the label above the control on phones, beside it on
 * computers (as in macOS), then help text and the error. The row shows the keyboard focus.
 */
export function FieldRow({ label, labelNote, hint, error, stacked = false, children }: FieldRowProps) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = [hint && hintId, error && errorId].filter(Boolean).join(' ') || undefined;

  return (
    <div
      className={cn(
        'row flex-col items-stretch gap-1',
        !stacked && 'lg:grid lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] lg:items-center lg:gap-x-4 lg:gap-y-1',
        'has-[:focus-visible]:outline-2 has-[:focus-visible]:-outline-offset-2 has-[:focus-visible]:outline-tint',
      )}
    >
      <label htmlFor={id} className={cn('text-subhead text-label-2', !stacked && 'lg:text-body lg:text-label')}>
        {label}
        {labelNote && <span className="text-label-2"> {labelNote}</span>}
      </label>
      <div className={cn('min-w-0', !stacked && 'lg:col-start-2')}>
        {children({ id, 'aria-describedby': describedBy, 'aria-invalid': error ? true : undefined })}
      </div>
      {hint && (
        <div id={hintId} className={cn('text-footnote text-label-2', !stacked && 'lg:col-start-2')}>
          {hint}
        </div>
      )}
      {error && (
        <FormError id={errorId} className={cn(!stacked && 'lg:col-start-2')}>
          {error}
        </FormError>
      )}
    </div>
  );
}

const INPUT_CLASSES =
  'w-full min-w-0 bg-transparent py-0.5 text-body text-label outline-none placeholder:text-label-3 focus-visible:outline-none disabled:opacity-40';

interface TextFieldProps
  extends FieldTextProps, Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'className' | 'id'> {
  value: string;
  onChange: (value: string) => void;
  /** Monospace, for keys and addresses. */
  mono?: boolean;
}

/** A single-line text field in a row. */
export function TextField({
  label,
  labelNote,
  hint,
  error,
  value,
  onChange,
  mono = false,
  type = 'text',
  ...inputProps
}: TextFieldProps) {
  return (
    <FieldRow label={label} labelNote={labelNote} hint={hint} error={error}>
      {(control) => (
        <input
          type={type}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          className={cn(INPUT_CLASSES, mono && 'font-mono')}
          {...control}
          {...inputProps}
        />
      )}
    </FieldRow>
  );
}

interface PasswordFieldProps
  extends
    FieldTextProps,
    Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'value' | 'onChange' | 'className' | 'id'> {
  value: string;
  onChange: (value: string) => void;
  /** Show the text at first, for Wi-Fi passwords people read out or type on other devices. */
  initiallyVisible?: boolean;
}

/** A password field with a show/hide button. */
export function PasswordField({
  label,
  labelNote,
  hint,
  error,
  value,
  onChange,
  initiallyVisible = false,
  ...inputProps
}: PasswordFieldProps) {
  const [visible, setVisible] = useState(initiallyVisible);

  return (
    <FieldRow label={label} labelNote={labelNote} hint={hint} error={error}>
      {(control) => (
        <div className="flex items-center gap-2">
          <input
            type={visible ? 'text' : 'password'}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            className={INPUT_CLASSES}
            spellCheck={false}
            autoCapitalize="none"
            autoCorrect="off"
            {...control}
            {...inputProps}
          />
          <IconButton
            icon={visible ? 'visibility_off' : 'visibility'}
            ariaLabel={visible ? 'Hide password' : 'Show password'}
            onClick={() => setVisible((isVisible) => !isVisible)}
            className="-my-1.5 -mr-1.5"
          />
        </div>
      )}
    </FieldRow>
  );
}

interface TextAreaFieldProps
  extends FieldTextProps, Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'value' | 'onChange' | 'className' | 'id'> {
  value: string;
  onChange: (value: string) => void;
  /** Monospace, for keys and configuration files. */
  mono?: boolean;
}

/** A multi-line text box in a row, e.g. for pasting SSH keys or a WireGuard file. */
export function TextAreaField({
  label,
  labelNote,
  hint,
  error,
  value,
  onChange,
  mono = false,
  rows = 5,
  ...textareaProps
}: TextAreaFieldProps) {
  return (
    <FieldRow label={label} labelNote={labelNote} hint={hint} error={error} stacked>
      {(control) => (
        <textarea
          rows={rows}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          spellCheck={false}
          autoCapitalize="none"
          autoCorrect="off"
          autoComplete="off"
          className={cn(
            'mt-1 w-full resize-y rounded-[10px] bg-fill-2 px-3 py-2 text-label outline-none placeholder:text-label-3 focus-visible:outline-none lg:mt-0',
            // Long keys wrap anywhere instead of scrolling sideways.
            mono ? 'font-mono text-footnote break-all' : 'text-body',
          )}
          {...control}
          {...textareaProps}
        />
      )}
    </FieldRow>
  );
}

interface SelectFieldProps<T extends string> extends FieldTextProps {
  options: readonly { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
  disabled?: boolean;
}

/** A choice from a menu in a row (the browser's own menu, which is native on every device). */
export function SelectField<T extends string>({ options, value, onChange, disabled, ...field }: SelectFieldProps<T>) {
  return (
    <FieldRow {...field}>
      {(control) => (
        <div className="relative flex items-center">
          <select
            value={value}
            disabled={disabled}
            onChange={(event) => onChange(event.target.value as T)}
            className={cn(INPUT_CLASSES, 'cursor-pointer appearance-none truncate pr-7')}
            {...control}
          >
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          <Icon name="unfold_more" size={18} className="pointer-events-none absolute right-0 text-label-2" />
        </div>
      )}
    </FieldRow>
  );
}

interface FileFieldProps extends FieldTextProps {
  /** File types the picker offers, e.g. ".pem,.crt". */
  accept: string;
  multiple?: boolean;
  /** The chosen files; empty when the choice was cleared. */
  onChange: (files: File[]) => void;
  disabled?: boolean;
}

/**
 * A file picker in a row: the browser's own control with an Apple-style button. A file input
 * can't be emptied from code, so give the field a new `key` to clear it after use.
 */
export function FileField({ accept, multiple = false, onChange, disabled, ...field }: FileFieldProps) {
  const [names, setNames] = useState<string[]>([]);

  return (
    <FieldRow
      {...field}
      // With several files the browser only says "2 files", so their names are listed.
      hint={
        multiple && names.length > 1 ? (
          <>
            {field.hint && <span className="block">{field.hint}</span>}
            Chosen: <span className="text-label">{names.join(', ')}</span>
          </>
        ) : (
          field.hint
        )
      }
    >
      {(control) => (
        <input
          type="file"
          accept={accept}
          multiple={multiple}
          disabled={disabled}
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            setNames(files.map((file) => file.name));
            onChange(files);
          }}
          className={cn(
            'mt-1 w-full min-w-0 cursor-pointer text-subhead text-label-2 outline-none focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-40 lg:mt-0',
            'file:mr-3 file:cursor-pointer file:rounded-full file:border-0 file:bg-fill-2 file:px-3.5 file:py-1.5 file:text-subhead file:font-semibold file:text-tint',
          )}
          {...control}
        />
      )}
    </FieldRow>
  );
}
