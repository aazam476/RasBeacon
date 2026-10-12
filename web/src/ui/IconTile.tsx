import { cn } from '@/lib/cn';
import { Icon, type BaseIconName } from './Icon';

/** The tile colors, as in the Settings app. */
export type TileColor = 'blue' | 'indigo' | 'teal' | 'green' | 'purple' | 'orange' | 'red' | 'gray';

const COLOR_CLASSES: Record<TileColor, string> = {
  blue: 'bg-tile-blue',
  indigo: 'bg-tile-indigo',
  teal: 'bg-tile-teal',
  green: 'bg-tile-green',
  purple: 'bg-tile-purple',
  orange: 'bg-tile-orange',
  red: 'bg-tile-red',
  gray: 'bg-tile-gray',
};

interface IconTileProps {
  icon: BaseIconName;
  color: TileColor;
  /** `row`: in front of a row (29pt on phones, 24pt on computers). `large`: on the More page. */
  size?: 'row' | 'large';
  className?: string;
}

/** A white symbol on a colored rounded square, in front of a row. Decorative. */
export function IconTile({ icon, color, size = 'row', className }: IconTileProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'row-tile grid shrink-0 place-items-center text-white',
        COLOR_CLASSES[color],
        size === 'row'
          ? 'size-[1.8125rem] rounded-[7px] lg:size-6 lg:rounded-[6px]'
          : 'size-9 rounded-[9px] lg:size-8 lg:rounded-[8px]',
        className,
      )}
    >
      <Icon
        name={icon}
        filled
        size={20}
        className={size === 'row' ? 'size-[1.1875rem] lg:size-4' : 'size-6 lg:size-[1.375rem]'}
      />
    </span>
  );
}
