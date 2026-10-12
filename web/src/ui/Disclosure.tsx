import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { Icon } from './Icon';
import { IconTile } from './IconTile';
import type { RowLeading } from './Section';

interface DisclosureProps {
  title: string;
  /** A tile or a plain symbol in front of the title. */
  leading?: RowLeading;
  /** Text under the title, e.g. what is inside. */
  subtitle?: ReactNode;
  defaultOpen?: boolean;
  /** `danger` draws the title in red, for destructive settings such as a factory reset. */
  tone?: 'default' | 'danger';
  /**
   * `rows` when the content is rows of the group (fields, switches): they continue the group
   * under the title. Otherwise the content is padded like a row's text.
   */
  content?: 'rows' | 'text';
  children: ReactNode;
}

/**
 * A row that opens to show more, for advanced or risky settings: they stay tucked away until
 * asked for. Use it as a row inside a `Section`.
 */
export function Disclosure({
  title,
  leading,
  subtitle,
  defaultOpen = false,
  tone = 'default',
  content = 'text',
  children,
}: DisclosureProps) {
  return (
    <details open={defaultOpen} className="row group/disclosure block p-0 lg:p-0">
      <summary
        className={cn(
          'row-pressable flex min-h-11 list-none items-center gap-3 px-4 py-2.5 lg:min-h-10 lg:gap-2.5 lg:px-3.5 lg:py-2',
          'focus-visible:-outline-offset-2 [&::-webkit-details-marker]:hidden',
        )}
      >
        {leading &&
          ('tile' in leading ? (
            <IconTile icon={leading.tile} color={leading.color} />
          ) : (
            <span className="row-tile grid w-[1.8125rem] shrink-0 place-items-center lg:w-6">
              <Icon
                name={leading.icon}
                size={22}
                className={cn('text-label-2 lg:size-[1.125rem]', leading.className)}
              />
            </span>
          ))}
        <span className="min-w-0 flex-1">
          <span className={cn('block text-body', tone === 'danger' ? 'text-red' : 'text-label')}>{title}</span>
          {subtitle && <span className="mt-0.5 block text-subhead text-label-2">{subtitle}</span>}
        </span>
        <Icon
          name="chevron_right"
          size={20}
          className="-mr-1 text-label-3 transition-transform duration-200 ease-out group-open/disclosure:rotate-90 motion-reduce:transition-none lg:size-4"
        />
      </summary>
      {content === 'rows' ? (
        <div className="row-group">{children}</div>
      ) : (
        <div className="flex flex-col gap-4 px-4 pt-1 pb-4 lg:px-3.5">{children}</div>
      )}
    </details>
  );
}
