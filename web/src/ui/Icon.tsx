import { cn } from '@/lib/cn';
import { ICON_PATHS, type IconName } from './icons';

/** Icon names without the `_fill` variants; pass `filled` to get one. */
export type BaseIconName = Exclude<IconName, `${string}_fill`>;
export type { IconName };

interface IconProps {
  name: BaseIconName;
  /** The filled glyph, where one exists (tiles, the selected tab, status symbols). */
  filled?: boolean;
  /**
   * Width and height in px at the default text size; icons grow with the text like SF Symbols
   * with Dynamic Type. Override with a `size-*` class (computers use smaller symbols).
   */
  size?: number;
  className?: string;
}

/** A Material Symbols Rounded icon drawn as inline SVG. It takes its color from the text color. */
export function Icon({ name, filled = false, size = 20, className }: IconProps) {
  const filledName = `${name}_fill` as IconName;
  const path = filled && filledName in ICON_PATHS ? ICON_PATHS[filledName] : ICON_PATHS[name];

  return (
    <svg
      viewBox="0 -960 960 960"
      width={`${size / 16}rem`}
      height={`${size / 16}rem`}
      fill="currentColor"
      aria-hidden="true"
      focusable="false"
      className={cn('shrink-0', className)}
    >
      <path d={path} />
    </svg>
  );
}
