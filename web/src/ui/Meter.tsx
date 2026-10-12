import { cn } from '@/lib/cn';

interface MeterProps {
  /** 0–100. */
  percent: number;
  /** What it measures, for screen readers: "CPU load". */
  label: string;
  /** `tint` by default; `orange` and `red` when a reading runs high; `gray` for secondary ones. */
  tone?: 'tint' | 'gray' | 'orange' | 'red';
  className?: string;
}

const TONE_CLASSES = {
  tint: 'bg-tint-fill',
  gray: 'bg-label-3',
  orange: 'bg-orange-fill',
  red: 'bg-red-fill',
};

/** A thin capsule filled to a level, like the storage bar in Settings. */
export function Meter({ percent, label, tone = 'tint', className }: MeterProps) {
  const value = Math.min(100, Math.max(0, percent));

  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(value)}
      className={cn('h-1.5 overflow-hidden rounded-full bg-fill-2', className)}
    >
      {/* A full-width fill clipped to the level: a new reading slides, and the end stays round. */}
      <div
        className={cn(
          'h-full transition-[clip-path] duration-300 ease-out motion-reduce:transition-none',
          TONE_CLASSES[tone],
        )}
        style={{ clipPath: `inset(0 ${100 - value}% 0 0 round 999px)` }}
      />
    </div>
  );
}
