import { cn } from '@/lib/cn';
import { IconButton } from './IconButton';

/**
 * A pencil next to a name, to rename it. With a mouse it shows while the pointer is over its
 * group (`group` on an ancestor) or while it has focus; on touch screens, which have no hover,
 * it always shows.
 */
export function EditButton({ label, onClick, className }: { label: string; onClick: () => void; className?: string }) {
  return (
    <IconButton
      icon="edit"
      ariaLabel={label}
      onClick={onClick}
      className={cn(
        '[@media(hover:hover)_and_(pointer:fine)]:opacity-0 [@media(hover:hover)_and_(pointer:fine)]:group-hover:opacity-100 [@media(hover:hover)_and_(pointer:fine)]:focus-visible:opacity-100',
        'transition-opacity duration-150',
        className,
      )}
    />
  );
}
