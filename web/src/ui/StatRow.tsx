import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';

export interface Stat {
  label: string;
  /** The number, large: "48.8". */
  value: string;
  /** Its unit, small after it: "Mbps". */
  unit?: string;
  /** A line under it: "Peak 92.1 Mbps". */
  caption?: ReactNode;
  /** Draws the number in the tint, for the reading that matters most. */
  highlight?: boolean;
}

/**
 * A few readings side by side in a row of a `Section`, with large numbers: download, upload
 * and ping, for example. When the row is too narrow for them (a small phone, large text) they
 * wrap onto more lines, and where a column can't fit "1285 Mbps" the unit goes under its
 * number, the same in every column.
 */
export function StatRow({ stats, className }: { stats: Stat[]; className?: string }) {
  // Four readings go two by two on phones rather than three and one.
  const pairs = stats.length === 4;
  return (
    <dl
      className={cn(
        'row grid items-start gap-x-4 gap-y-3 py-3 lg:py-2.5',
        pairs && 'grid-cols-2 sm:grid-cols-4',
        className,
      )}
      style={pairs ? undefined : { gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 4.5rem), 1fr))' }}
    >
      {stats.map((stat) => (
        <div key={stat.label} className="@container flex min-w-0 flex-col">
          <dt className="text-footnote text-label-2">{stat.label}</dt>
          <dd className="selectable flex flex-wrap items-baseline gap-x-1">
            <span className={cn('tabular text-title2 font-semibold', stat.highlight ? 'text-tint' : 'text-label')}>
              {stat.value}
            </span>
            {stat.unit && (
              <span className="text-footnote font-medium text-label-2 @max-[5.75rem]:basis-full">{stat.unit}</span>
            )}
          </dd>
          {stat.caption && <dd className="text-footnote text-label-2">{stat.caption}</dd>}
        </div>
      ))}
    </dl>
  );
}
