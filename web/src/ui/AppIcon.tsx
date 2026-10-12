import { cn } from '@/lib/cn';
import { Icon } from './Icon';

const SIZE_CLASSES = {
  /** The desktop sidebar. */
  sm: 'size-7 [&_svg]:size-[1.125rem]',
  /** Full-page screens: login, setup, notices. */
  lg: 'size-[4.5rem] [&_svg]:size-11',
};

/** RasBeacon's icon, a beacon on a teal rounded square as on a home screen. Decorative. */
export function AppIcon({ size = 'sm', className }: { size?: keyof typeof SIZE_CLASSES; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn('app-icon grid shrink-0 place-items-center text-white', SIZE_CLASSES[size], className)}
    >
      <Icon name="wifi_tethering" />
    </span>
  );
}
