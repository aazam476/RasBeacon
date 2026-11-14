import { cn } from '@/lib/cn';
import { IconButton } from './IconButton';

interface EditButtonProps {
  label: string;
  onClick: () => void;
  /**
   * `text` (default): right after a name, only as big as the pencil, so on a line of its own it
   * lines up with the name. `buttons`: among other icon buttons, the same size as they are.
   */
  beside?: 'text' | 'buttons';
  className?: string;
}

/**
 * A pencil next to a name, to rename it. With a mouse it shows while the pointer is over its
 * group (`group` on an ancestor) or while it has focus; on touch screens, which have no hover,
 * it always shows.
 */
export function EditButton({ label, onClick, beside = 'text', className }: EditButtonProps) {
  return (
    <IconButton
      icon="edit"
      ariaLabel={label}
      onClick={onClick}
      tight={beside === 'text'}
      className={cn(
        '[@media(hover:hover)_and_(pointer:fine)]:opacity-0 [@media(hover:hover)_and_(pointer:fine)]:group-hover:opacity-100 [@media(hover:hover)_and_(pointer:fine)]:focus-visible:opacity-100',
        'transition-opacity duration-150',
        className,
      )}
    />
  );
}
