import { createContext, useContext, useId, type ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { useAction } from '@/lib/useAction';
import { useErrorToast } from './errorToastContext';
import { Icon, type BaseIconName } from './Icon';

interface ChoiceListState {
  name: string;
  value: string;
  onChange: (value: string) => void;
}

const ChoiceListContext = createContext<ChoiceListState | null>(null);

interface ChoiceListProps<T extends string> {
  /** Name read by screen readers, e.g. "WireGuard profile". */
  ariaLabel: string;
  value: T;
  /**
   * Called with the picked option. A device action returns its promise: until the router
   * answers, the list dims and ignores presses, and the check mark moves once the state
   * changes. A failure appears in the error toast.
   */
  onChange: (value: T) => void | Promise<void>;
  /** The `Choice` rows. */
  children: ReactNode;
}

/**
 * Rows where exactly one is chosen, marked with a check, as in the Settings app. Built on
 * native radio buttons, so arrow keys move between them.
 */
export function ChoiceList<T extends string>({ ariaLabel, value, onChange, children }: ChoiceListProps<T>) {
  const name = useId();
  const choosing = useAction(async (next: T) => onChange(next), useErrorToast());
  // Options report plain strings; they are always one of the T values passed to them.
  const state: ChoiceListState = { name, value, onChange: (next) => choosing.run(next as T) };

  return (
    <ChoiceListContext value={state}>
      <div
        role="radiogroup"
        aria-label={ariaLabel}
        aria-busy={choosing.pending || undefined}
        className={cn('rows', choosing.pending && 'cursor-wait opacity-60')}
      >
        {children}
      </div>
    </ChoiceListContext>
  );
}

interface ChoiceProps {
  value: string;
  title: ReactNode;
  description?: ReactNode;
  /** A symbol in front of the title, e.g. a file for configuration files. */
  icon?: BaseIconName;
  /** Monospace title, for file names. */
  mono?: boolean;
  /** Content on the right outside the pressable part, e.g. a Remove button. */
  trailing?: ReactNode;
}

export function Choice({ value, title, description, icon, mono = false, trailing }: ChoiceProps) {
  const list = useContext(ChoiceListContext);
  if (!list) {
    throw new Error('<Choice> must be used inside <ChoiceList>.');
  }
  const selected = list.value === value;

  return (
    <div className="row gap-0 py-0 pr-2 pl-0 lg:py-0 lg:pr-1.5 lg:pl-0">
      <label className="row-pressable relative flex min-h-11 min-w-0 flex-1 cursor-pointer items-center gap-3 self-stretch py-2.5 pl-4 lg:min-h-10 lg:gap-2.5 lg:py-2 lg:pl-3.5 has-[input:focus-visible]:outline-2 has-[input:focus-visible]:-outline-offset-2 has-[input:focus-visible]:outline-tint">
        <input
          type="radio"
          name={list.name}
          value={value}
          checked={selected}
          onChange={() => list.onChange(value)}
          // Transparent but covering the row, so a press anywhere lands on the radio itself.
          className="absolute inset-0 size-full cursor-pointer appearance-none opacity-0"
        />
        {icon && (
          <span className="row-tile grid w-[1.8125rem] shrink-0 place-items-center lg:w-6">
            <Icon name={icon} size={22} className="text-label-2 lg:size-[1.125rem]" />
          </span>
        )}
        <span className="min-w-0 flex-1">
          <span className={cn('block wrap-anywhere text-label', mono ? 'font-mono text-callout' : 'text-body')}>
            {title}
          </span>
          {description && <span className="mt-0.5 block text-subhead wrap-anywhere text-label-2">{description}</span>}
        </span>
        <Icon
          name="check"
          size={22}
          className={cn('shrink-0 text-tint lg:size-[1.125rem]', selected ? 'opacity-100' : 'opacity-0')}
        />
      </label>
      {trailing}
    </div>
  );
}
