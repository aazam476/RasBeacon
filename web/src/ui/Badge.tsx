import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';

/** What a state means: good, needs attention, bad, or nothing in particular. */
export type Tone = 'green' | 'orange' | 'red' | 'gray' | 'tint';

// Light tints keep the text above 4.5:1 contrast, on the page and in rows alike.
const TONE_CLASSES: Record<Tone, string> = {
  green: 'bg-green-fill/12 text-green',
  orange: 'bg-orange-fill/12 text-orange',
  red: 'bg-red-fill/12 text-red',
  gray: 'bg-fill-2 text-label-2',
  tint: 'bg-tint/12 text-tint',
};

interface BadgeProps {
  tone?: Tone;
  /** A pulsing dot in front of the text, for states that are still changing ("Connecting"). */
  pulse?: boolean;
  className?: string;
  children: ReactNode;
}

/** A short state in a capsule, such as "Connected" or "This device". */
export function Badge({ tone = 'gray', pulse = false, className, children }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-footnote font-semibold whitespace-nowrap',
        TONE_CLASSES[tone],
        className,
      )}
    >
      {pulse && (
        <span
          aria-hidden="true"
          className="size-1.5 animate-pulse rounded-full bg-current motion-reduce:animate-none"
        />
      )}
      {children}
    </span>
  );
}
