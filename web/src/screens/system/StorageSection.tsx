import type { StorageInfo } from '@/data/types';
import { cn } from '@/lib/cn';
import { percentOf } from '@/lib/format';
import { RowText, Section } from '@/ui';

/**
 * The SD card as one bar split by what uses it, like iPhone Storage, with a legend. The plugins'
 * data is a part of its own (the space is set aside, used or not), and the footer says how much
 * each plugin takes.
 */
export function StorageSection({ storage }: { storage: StorageInfo }) {
  const plugins = storage.plugins ?? [];
  const pluginsGb = plugins.reduce((sum, plugin) => sum + plugin.gb, 0);
  const free = storage.totalGb - storage.firmwareGb - storage.userGb - pluginsGb;
  const segments = [
    { label: 'RasBeacon', gb: storage.firmwareGb, color: 'bg-tile-indigo' },
    { label: 'Settings and logs', gb: storage.userGb, color: 'bg-tile-teal' },
    ...(plugins.length > 0 ? [{ label: 'Plugins', gb: pluginsGb, color: 'bg-tile-orange' }] : []),
  ];
  const footer =
    plugins.length > 0
      ? `Plugins: ${plugins.map((plugin) => `${plugin.name} ${plugin.gb.toFixed(1)} GB`).join(', ')}.`
      : undefined;

  return (
    <Section title="Storage" footer={footer}>
      <div className="row flex-col items-stretch gap-3 py-3.5">
        <RowText
          title={storage.medium}
          value={<span className="tabular">{`${free.toFixed(1)} GB of ${storage.totalGb.toFixed(1)} GB free`}</span>}
        />
        <div aria-hidden="true" className="flex h-2.5 w-full gap-0.5 overflow-hidden rounded-full bg-fill-2">
          {segments.map((segment) => (
            <div
              key={segment.label}
              className={cn('h-full first:rounded-l-full', segment.color)}
              style={{ width: `${percentOf(segment.gb, storage.totalGb)}%` }}
            />
          ))}
        </div>
        <dl className="flex flex-wrap gap-x-5 gap-y-1">
          {segments.map((segment) => (
            <div key={segment.label} className="flex items-center gap-1.5 text-footnote">
              <span aria-hidden="true" className={cn('size-2 rounded-full', segment.color)} />
              <dt className="text-label-2">{segment.label}</dt>
              <dd className="tabular text-label">{segment.gb.toFixed(1)} GB</dd>
            </div>
          ))}
        </dl>
      </div>
    </Section>
  );
}
