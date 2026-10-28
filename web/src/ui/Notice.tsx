import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { Icon, type BaseIconName } from './Icon';

/** What a notice is about: information, something to look at, a problem, or good news. */
export type NoticeTone = 'info' | 'warning' | 'danger' | 'success' | 'neutral';

const TONE: Record<NoticeTone, { surface: string; icon: string; defaultIcon: BaseIconName }> = {
  info: { surface: 'bg-tint/12', icon: 'text-tint', defaultIcon: 'info' },
  warning: { surface: 'bg-orange-fill/14', icon: 'text-orange', defaultIcon: 'warning' },
  danger: { surface: 'bg-red-fill/12', icon: 'text-red', defaultIcon: 'error' },
  success: { surface: 'bg-green-fill/14', icon: 'text-green', defaultIcon: 'check_circle' },
  neutral: { surface: 'bg-cell', icon: 'text-label-2', defaultIcon: 'info' },
};

interface NoticeProps {
  tone?: NoticeTone;
  icon?: BaseIconName;
  title?: ReactNode;
  /** `alert` for problems that appear while the page is open; `status` for progress. */
  role?: 'alert' | 'status';
  /** Buttons or links under the text. */
  actions?: ReactNode;
  className?: string;
  children?: ReactNode;
}

interface NoticeListProps {
  tone?: NoticeTone;
  /** Names the list for screen readers, e.g. "Warnings". */
  ariaLabel: string;
  /** One sentence each. Each is announced when it appears. */
  messages: readonly string[];
}

/**
 * Several messages of one kind as one tinted group, with a line between them like rows, so a
 * long list stays one calm block instead of a stack of boxes.
 */
export function NoticeList({ tone = 'info', ariaLabel, messages }: NoticeListProps) {
  const style = TONE[tone];

  return (
    <ul aria-label={ariaLabel} className={cn('notice-list flex flex-col rounded-group', style.surface)}>
      {/* The same message can come twice (two devices with the same problem): the place makes keys unique. */}
      {messages.map((message, i) => (
        <li key={`${i}:${message}`} className="relative flex gap-3 px-4 py-3.5 lg:px-3.5 lg:py-3">
          <Icon name={style.defaultIcon} filled size={22} className={cn('mt-px lg:size-5', style.icon)} />
          <p role="alert" className="min-w-0 flex-1 text-subhead wrap-break-word text-label">
            {message}
          </p>
        </li>
      ))}
    </ul>
  );
}

/**
 * A message that stands on its own between the groups: a warning, a problem, a confirmation.
 * Its tint and symbol say what kind it is; the words say what to do.
 */
export function Notice({ tone = 'info', icon, title, role, actions, className, children }: NoticeProps) {
  const style = TONE[tone];

  return (
    <div
      role={role}
      className={cn(
        '@container flex w-full min-w-0 gap-3 rounded-group px-4 py-3.5 lg:px-3.5 lg:py-3',
        style.surface,
        className,
      )}
    >
      {/* With very large text on a narrow screen the symbol gives its room to the words. */}
      <Icon
        name={icon ?? style.defaultIcon}
        filled
        size={22}
        className={cn('mt-px shrink-0 lg:size-5 @max-[16rem]:hidden', style.icon)}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {title && <p className="text-headline wrap-break-word text-label">{title}</p>}
        {children && <div className="flex flex-col gap-1 text-subhead wrap-break-word text-label">{children}</div>}
        {/* Buttons sit on solid capsules here, so their text keeps its contrast on the tint. */}
        {actions && (
          <div className="mt-2 flex flex-wrap items-center gap-2 [--c-fill-2:var(--c-cell)] [--c-fill:var(--c-cell)]">
            {actions}
          </div>
        )}
      </div>
    </div>
  );
}
