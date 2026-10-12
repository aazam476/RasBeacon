import { cn } from '@/lib/cn';

interface ActivityIndicatorProps {
  /** Width and height in px at the default text size; it grows with the text, like icons. */
  size?: number;
  /** Text after the indicator, read out as a status ("Loading the status…"). */
  label?: string;
  className?: string;
}

/**
 * The spinning wheel of eight bars that iOS shows while something loads. It turns quickly:
 * a fast spinner makes waiting feel shorter.
 */
export function ActivityIndicator({ size = 20, label, className }: ActivityIndicatorProps) {
  const wheel = (
    <svg
      viewBox="0 0 24 24"
      width={`${size / 16}rem`}
      height={`${size / 16}rem`}
      aria-hidden="true"
      className="activity-spin shrink-0"
    >
      {Array.from({ length: 8 }, (_, index) => (
        <rect
          key={index}
          x="10.75"
          y="1.5"
          width="2.5"
          height="6.5"
          rx="1.25"
          fill="currentColor"
          opacity={1 - index * 0.11}
          transform={`rotate(${-index * 45} 12 12)`}
        />
      ))}
    </svg>
  );

  if (!label) return <span className={cn('inline-flex text-label-2', className)}>{wheel}</span>;

  return (
    <span role="status" className={cn('inline-flex items-center gap-2 text-label-2', className)}>
      {wheel}
      <span>{label}</span>
    </span>
  );
}
