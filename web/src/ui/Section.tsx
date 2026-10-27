import { useId, type ReactNode } from 'react';
import { Link } from 'react-router';
import { cn } from '@/lib/cn';
import { useAction } from '@/lib/useAction';
import { ActivityIndicator } from './ActivityIndicator';
import { useErrorToast } from './errorToastContext';
import { Icon, type BaseIconName } from './Icon';
import { IconTile, type TileColor } from './IconTile';
import { FormSwitch, Switch } from './Switch';

interface SectionProps {
  /** The heading above the group, in sentence case. */
  title?: ReactNode;
  /** The heading's level: 2 on pages, 3 inside sheets (whose title is the h2). */
  level?: 2 | 3;
  /** A small action on the heading's line, such as "Edit". */
  action?: ReactNode;
  /** Explanation under the group. */
  footer?: ReactNode;
  /** The rows. Anything else that is not a row should come with its own padding. */
  children: ReactNode;
  className?: string;
}

/**
 * A group of rows on a rounded surface, with an optional heading above and an explanation
 * below: the building block of every page, as in the Settings app. The heading is a real
 * heading for navigating by headings; the group isn't a labelled region, so pages don't fill
 * up with landmarks and a group named like a field ("Admin password") never matches the field.
 */
export function Section({ title, level = 2, action, footer, children, className }: SectionProps) {
  const Heading = level === 2 ? 'h2' : 'h3';

  return (
    <section className={cn('min-w-0', className)}>
      {(title || action) && (
        // The action goes under the heading when both don't fit (large text).
        <div className="flex min-h-7 flex-wrap items-end justify-between gap-x-3 gap-y-1 px-4 pb-1.5 lg:px-1">
          {title && <Heading className="text-subhead font-semibold wrap-break-word text-label-2">{title}</Heading>}
          {action && <div className={cn('flex items-center gap-2', !title && 'ml-auto')}>{action}</div>}
        </div>
      )}
      <div className="overflow-hidden rounded-group bg-cell">{children}</div>
      {footer && <div className="px-4 pt-1.5 text-footnote text-label-2 lg:px-1">{footer}</div>}
    </section>
  );
}

/**
 * The leading part of a row: a colored tile, a plain symbol, or a picture (a plugin's icon).
 * A picture that is missing shows as a gray tile with `fallback` (a puzzle piece by default).
 */
export type RowLeading =
  | { tile: BaseIconName; color: TileColor }
  | { icon: BaseIconName; className?: string }
  | { image: string; fallback?: BaseIconName };

export function Leading({ leading }: { leading?: RowLeading }) {
  if (!leading) return null;
  if ('tile' in leading) return <IconTile icon={leading.tile} color={leading.color} />;
  if ('image' in leading && !leading.image) return <IconTile icon={leading.fallback ?? 'extension'} color="gray" />;
  if ('image' in leading) {
    return (
      <img
        src={leading.image}
        alt=""
        draggable={false}
        className="row-tile size-[1.8125rem] shrink-0 rounded-[7px] object-cover lg:size-6 lg:rounded-[6px]"
      />
    );
  }
  return (
    <span className="row-tile grid w-[1.8125rem] shrink-0 place-items-center lg:w-6">
      <Icon name={leading.icon} size={22} className={cn('text-label-2 lg:size-[1.125rem]', leading.className)} />
    </span>
  );
}

interface RowTextProps {
  title: ReactNode;
  /** A second line under the title. */
  subtitle?: ReactNode;
  /** The value on the right ("Connected", "5 GHz"). It moves under the title when space runs out. */
  value?: ReactNode;
  titleId?: string;
  subtitleId?: string;
  /** Classes for the title, e.g. a tone. */
  titleClassName?: string;
}

function hasValue(value: ReactNode): boolean {
  return value !== undefined && value !== null && value !== '';
}

/**
 * The line with a row's title and its value: the value sits on the right while both fit, and
 * otherwise goes under the title, lined up with it, as iOS does at large text sizes. The
 * subtitle goes below the line, so a long one doesn't push the value down.
 */
function TitleLine({ title, value, valueId }: { title: ReactNode; value?: ReactNode; valueId?: string }) {
  return (
    <span className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
      {title}
      {hasValue(value) && (
        <span id={valueId} className="selectable max-w-full text-body wrap-break-word text-label-2">
          {value}
        </span>
      )}
    </span>
  );
}

/** Title, subtitle and value of a row. */
export function RowText({ title, subtitle, value, titleId, subtitleId, titleClassName }: RowTextProps) {
  return (
    <span className="min-w-0 flex-1">
      <TitleLine
        title={
          <span id={titleId} className={cn('min-w-0 text-body wrap-break-word text-label', titleClassName)}>
            {title}
          </span>
        }
        value={value}
      />
      {subtitle && (
        <span id={subtitleId} className="mt-0.5 block text-subhead wrap-break-word text-label-2">
          {subtitle}
        </span>
      )}
    </span>
  );
}

interface RowProps extends RowTextProps {
  leading?: RowLeading;
  /** Content after the text: a badge, a switch, a small button. */
  trailing?: ReactNode;
  /** Content below the row's line, inside the row (details, messages). */
  children?: ReactNode;
  className?: string;
}

/** A row that only shows something. */
export function Row({ leading, trailing, children, className, ...text }: RowProps) {
  if (children) {
    return (
      <div className={cn('row flex-col items-stretch gap-2', className)}>
        <div className="flex items-center gap-3 lg:gap-2.5">
          <Leading leading={leading} />
          <RowText {...text} />
          {trailing}
        </div>
        {/* Lined up with the text, past the leading symbol. */}
        <div className={cn(leading && 'pl-[2.5625rem] lg:pl-[2.125rem]')}>{children}</div>
      </div>
    );
  }
  return (
    <div className={cn('row', className)}>
      <Leading leading={leading} />
      <RowText {...text} />
      {trailing}
    </div>
  );
}

interface LinkRowProps {
  /** A page in the app (`/vpn`) or, with `external` or `reload`, a web address. */
  to: string;
  /** Opens in a new tab, with an arrow instead of the chevron. */
  external?: boolean;
  /** Loads the address in this tab (a full page load rather than a page of the app). */
  reload?: boolean;
  leading?: RowLeading;
  /** The link's name. */
  title: ReactNode;
  /** A second line under the title. */
  subtitle?: ReactNode;
  /** The state on the right, e.g. "On". */
  value?: ReactNode;
  className?: string;
}

/**
 * A row that goes somewhere, with a chevron. The whole row is pressable, but the link's name is
 * only its title; the subtitle and the value are read as its description, so "Hotspot" stays
 * "Hotspot" whatever its state.
 */
export function LinkRow({
  to,
  external = false,
  reload = false,
  leading,
  title,
  subtitle,
  value,
  className,
}: LinkRowProps) {
  const id = useId();
  const describedBy =
    [subtitle && `${id}-subtitle`, hasValue(value) && `${id}-value`].filter(Boolean).join(' ') || undefined;
  // The link covers the row with its ::after layer; the row shows its focus ring.
  const linkClasses =
    'min-w-0 text-body wrap-break-word text-label no-underline after:absolute after:inset-0 focus-visible:outline-none';

  return (
    <div
      className={cn(
        'row row-pressable',
        'has-[a:focus-visible]:rounded-group has-[a:focus-visible]:outline-2 has-[a:focus-visible]:-outline-offset-2 has-[a:focus-visible]:outline-tint',
        className,
      )}
    >
      <Leading leading={leading} />
      <span className="min-w-0 flex-1">
        <TitleLine
          title={
            external || reload ? (
              <a
                href={to}
                target={external ? '_blank' : undefined}
                rel={external ? 'noreferrer' : undefined}
                aria-describedby={describedBy}
                className={linkClasses}
              >
                {title}
              </a>
            ) : (
              <Link to={to} aria-describedby={describedBy} className={linkClasses}>
                {title}
              </Link>
            )
          }
          value={value}
          valueId={`${id}-value`}
        />
        {subtitle && (
          <span id={`${id}-subtitle`} className="mt-0.5 block text-subhead wrap-break-word text-label-2">
            {subtitle}
          </span>
        )}
      </span>
      <Icon
        name={external ? 'open_in_new' : 'chevron_right'}
        size={external ? 18 : 20}
        className="-mr-1 text-label-3 lg:size-4"
      />
    </div>
  );
}

interface SheetRowProps {
  /** What it opens, e.g. "Fixed address". */
  title: ReactNode;
  /** The state on the right, e.g. "Off" or "10.87.0.50". */
  value?: ReactNode;
  leading?: RowLeading;
  /** Opens the sheet. */
  onClick: () => void;
  className?: string;
}

/**
 * A row that opens a sheet over the page, with a chevron like a row that opens a page
 * (`LinkRow`): its name in the label color and its state on the right. The row's name is its
 * title; the value is read as its description.
 */
export function SheetRow({ title, value, leading, onClick, className }: SheetRowProps) {
  const id = useId();
  return (
    <button
      type="button"
      onClick={onClick}
      aria-labelledby={`${id}-title`}
      aria-describedby={hasValue(value) ? `${id}-value` : undefined}
      aria-haspopup="dialog"
      className={cn('row row-pressable w-full text-left', className)}
    >
      <Leading leading={leading} />
      {/* The title and value share the line while they fit; with large text the value moves
          under the title (to the right), rather than squeezing it a letter wide. */}
      <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-3">
        <span id={`${id}-title`} className="min-w-0 grow text-body wrap-break-word text-label">
          {title}
        </span>
        {hasValue(value) && (
          <span id={`${id}-value`} className="ml-auto min-w-0 text-right text-body wrap-break-word text-label-2">
            {value}
          </span>
        )}
      </span>
      <Icon name="chevron_right" size={20} className="-mr-1 shrink-0 text-label-3 lg:size-4" />
    </button>
  );
}

interface ButtonRowProps {
  /** A device action (the row shows the indicator while it runs; failures go to the toast)… */
  action?: () => Promise<void>;
  /** …or a plain click handler (opening a sheet), with `busy` when it starts work of its own. */
  onClick?: () => void;
  busy?: boolean;
  /** `tint` for actions, `danger` for disruptive ones. */
  tone?: 'tint' | 'danger';
  leading?: RowLeading;
  /** Label while the action runs, e.g. "Looking for networks…". */
  busyLabel?: string;
  disabled?: boolean;
  /** Text on the right, e.g. a count. */
  value?: ReactNode;
  /** A short line under the label, e.g. why it is grayed out ("Needs an internet connection"). */
  note?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** A row that does something when pressed: its label is in the tint, or red. */
export function ButtonRow({
  action,
  onClick,
  busy: busyProp = false,
  tone = 'tint',
  leading,
  busyLabel,
  disabled = false,
  value,
  note,
  children,
  className,
}: ButtonRowProps) {
  const running = useAction(action ?? (async () => {}), useErrorToast());
  const busy = running.pending || busyProp;

  return (
    <button
      type="button"
      disabled={disabled}
      aria-busy={busy || undefined}
      onClick={() => {
        if (busy) return;
        if (action) running.run();
        else onClick?.();
      }}
      className={cn(
        'row row-pressable w-full text-left disabled:cursor-not-allowed disabled:opacity-40',
        busy && 'cursor-wait',
        className,
      )}
    >
      <Leading leading={leading} />
      <span className={cn('min-w-0 flex-1 text-body wrap-break-word', tone === 'danger' ? 'text-red' : 'text-tint')}>
        {busy && busyLabel ? busyLabel : children}
        {note && <span className="block text-subhead text-label-2">{note}</span>}
      </span>
      {hasValue(value) && <span className="text-body text-label-2">{value}</span>}
      {busy && <ActivityIndicator size={18} />}
    </button>
  );
}

interface SwitchRowProps {
  title: ReactNode;
  subtitle?: ReactNode;
  leading?: RowLeading;
  checked: boolean;
  /** The device action; the switch moves when the router has done it. */
  onChange: (checked: boolean) => Promise<void>;
  disabled?: boolean;
  className?: string;
}

/**
 * A setting that applies as soon as its switch is flipped. The title names the switch.
 *
 * With very large text on a narrow phone (200% on a 320 px screen), the icon and the switch grow
 * too and leave the title a few letters wide. So when the row itself is narrower than 14rem (which
 * only happens with large text: rem grows with the text size), it does what iOS does: the icon
 * goes, and the switch moves to its own line under the text.
 */
export function SwitchRow({ title, subtitle, leading, checked, onChange, disabled, className }: SwitchRowProps) {
  const id = useId();

  return (
    <div className={cn('row @container flex-wrap', className)}>
      {/* `contents`: the wrapper adds no box, so the row still lines up with its icon. */}
      <span className="contents @max-[14rem]:hidden">
        <Leading leading={leading} />
      </span>
      <RowText title={title} subtitle={subtitle} titleId={`${id}-title`} subtitleId={`${id}-subtitle`} />
      <span className="flex @max-[14rem]:basis-full">
        <Switch
          checked={checked}
          onChange={onChange}
          labelledBy={`${id}-title`}
          describedBy={subtitle ? `${id}-subtitle` : undefined}
          disabled={disabled}
        />
      </span>
    </div>
  );
}

interface FormSwitchRowProps {
  /** The switch's label, e.g. "Turn on the guest network". */
  label: ReactNode;
  /** More of the label in the secondary color; screen readers read it as part of the name. */
  labelNote?: ReactNode;
  description?: ReactNode;
  leading?: RowLeading;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  /** Shown under the row's line. */
  children?: ReactNode;
  className?: string;
}

/** A setting in a form, saved with it. Pressing anywhere on the row flips the switch. */
export function FormSwitchRow({
  label,
  labelNote,
  description,
  leading,
  checked,
  onChange,
  disabled,
  children,
  className,
}: FormSwitchRowProps) {
  const id = useId();

  return (
    // As SwitchRow: with very large text on a narrow row the icon goes and the switch moves under
    // its label.
    <div className={cn('row @container flex-col items-stretch gap-2', className)}>
      <label
        className={cn(
          'flex flex-wrap items-center gap-3 lg:gap-2.5',
          disabled ? 'cursor-not-allowed' : 'cursor-pointer',
        )}
      >
        <span className="contents @max-[14rem]:hidden">
          <Leading leading={leading} />
        </span>
        <span className="min-w-0 flex-1 @max-[14rem]:basis-full">
          <span id={`${id}-label`} className="block text-body wrap-break-word text-label">
            {label}
            {labelNote && <span className="text-label-2"> {labelNote}</span>}
          </span>
          {description && (
            <span id={`${id}-description`} className="mt-0.5 block text-subhead text-label-2">
              {description}
            </span>
          )}
        </span>
        <FormSwitch
          checked={checked}
          onChange={onChange}
          disabled={disabled}
          labelledBy={`${id}-label`}
          describedBy={description ? `${id}-description` : undefined}
        />
      </label>
      {children}
    </div>
  );
}
