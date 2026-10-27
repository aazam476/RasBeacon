import { useLayoutEffect, useRef } from 'react';
import type { Status } from '@/data/types';
import { cn } from '@/lib/cn';
import { formatMbps } from '@/lib/format';
import { prefersReducedMotion } from '@/lib/spring';
import { Icon, type Tone } from '@/ui';
import { vpnStateView, phoneInUse, phoneName } from '../common/describe';

const SYMBOL_CLASSES: Record<Tone, string> = {
  green: 'bg-green-fill/15 text-green',
  orange: 'bg-orange-fill/15 text-orange',
  red: 'bg-red-fill/15 text-red',
  gray: 'bg-fill-2 text-label-2',
  tint: 'bg-tint/15 text-tint',
};

/**
 * The top of the Status page: whether your devices are protected, as a large shield and a word
 * or two, then the sentence that explains it. The two failures stand out: orange when the kill
 * switch leaves devices offline, red when they use the internet without the VPN. On computers
 * the internet connection, the devices and the traffic sit beside it.
 */
export function StatusHero({ status }: { status: Status }) {
  const view = vpnStateView(status.vpn);
  const symbolRef = useRef<HTMLSpanElement>(null);
  const shown = useRef(view.title);

  // When the state changes, the new one settles in, softly, so the change is noticed.
  useLayoutEffect(() => {
    if (shown.current === view.title) return;
    shown.current = view.title;
    const symbol = symbolRef.current;
    if (!symbol) return;
    symbol.animate(
      prefersReducedMotion()
        ? [{ opacity: 0 }, { opacity: 1 }]
        : [
            { opacity: 0, transform: 'scale(0.9)', filter: 'blur(3px)' },
            { opacity: 1, transform: 'none', filter: 'none' },
          ],
      { duration: 300, easing: 'cubic-bezier(0.23, 1, 0.32, 1)' },
    );
  }, [view.title]);

  return (
    <section className="@container flex flex-col items-center gap-3 rounded-group bg-cell px-5 pt-6 pb-5 text-center lg:flex-row lg:gap-5 lg:px-6 lg:py-5 lg:text-left">
      <span
        ref={symbolRef}
        aria-hidden="true"
        className={cn(
          'grid size-[4.5rem] shrink-0 place-items-center rounded-full lg:size-16',
          SYMBOL_CLASSES[view.tone],
        )}
      >
        <Icon name={view.icon} filled size={40} className="lg:size-9" />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1" role={view.alert ? 'alert' : undefined}>
        <h2 className="text-title2 text-label">{view.title}</h2>
        <p className="text-subhead text-pretty text-label-2">{view.summary}</p>
      </div>
      <Facts status={status} />
    </section>
  );
}

/**
 * Three readings beside the state where the hero is wide enough for them (large computer
 * screens at normal text sizes); otherwise the groups below hold them.
 */
function Facts({ status }: { status: Status }) {
  const { uplink } = status;
  const devices = status.clients.length + status.guest.clients.length;
  let internet = 'Not connected';
  if (uplink.connected) {
    if (uplink.kind === 'ethernet') internet = 'Ethernet';
    else if (uplink.kind === 'phone') internet = phoneName(phoneInUse(uplink));
    else internet = uplink.wifi.ssid || 'Wi-Fi';
  }

  const facts = [
    { label: 'Internet', value: internet },
    { label: 'Devices', value: String(devices) },
    { label: 'Download', value: formatMbps(status.throughput.downMbps) },
  ];

  return (
    <dl className="hidden shrink-0 grid-cols-[repeat(3,auto)] divide-x divide-separator text-left @[56rem]:grid">
      {facts.map((fact) => (
        <div key={fact.label} className="flex max-w-48 min-w-0 flex-col gap-0.5 px-5 last:pr-1">
          <dt className="text-footnote text-label-2">{fact.label}</dt>
          <dd className="selectable truncate text-headline text-label" title={fact.value}>
            {fact.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
