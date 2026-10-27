import { useId, useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { cn } from '@/lib/cn';
import { mbpsValue } from '@/lib/format';
import { Loaded } from '@/shell/Loaded';
import { ButtonRow, Icon, Row, Section, StatRow } from '@/ui';
import { allClients, type NetworkClient } from '../common/clientDisplay';
import { ClientSection } from './ClientSection';
import { UsageSection } from './UsageSection';
import { CLIENT_FILTERS, filterClients, priorityFirst, type ClientFilter } from './clientFilters';
import { PriorityNotice } from './PriorityNotice';

/** Every device on RasBeacon's networks, with live readings and a switch for its internet access. */
export function ClientsScreen() {
  const { status, refreshStatus } = useDevice();

  return (
    <Loaded data={status} what="the clients" onRetry={refreshStatus}>
      {(status) => <ClientList status={status} />}
    </Loaded>
  );
}

function ClientList({ status }: { status: Status }) {
  const { refreshStatus } = useDevice();
  const [filter, setFilter] = useState<ClientFilter>('all');
  const clients = allClients(status);
  const shown = priorityFirst(filterClients(clients, filter));

  return (
    <>
      <Section footer={`Subnet ${status.lan.subnet}. DHCP hands out ${status.lan.dhcpRange}.`}>
        <StatRow
          stats={[
            { label: 'Devices', value: String(clients.length), unit: `of ${status.lan.maxClients}` },
            { label: 'Download', value: mbpsValue(status.throughput.downMbps), unit: 'Mbps', highlight: true },
            { label: 'Upload', value: mbpsValue(status.throughput.upMbps), unit: 'Mbps' },
          ]}
        />
        <Row title="On your network" value={status.clients.length} />
        <Row title="On the guest network" value={status.guest.clients.length} />
        <ButtonRow action={refreshStatus} busyLabel="Refreshing…">
          Refresh
        </ButtonRow>
      </Section>

      <PriorityNotice />
      {clients.length === 0 ? (
        <NoDevices status={status} />
      ) : (
        <FilterChips clients={clients} value={filter} onChange={setFilter} />
      )}

      {clients.length === 0 ? null : shown.length === 0 ? (
        <p className="py-6 text-center text-body text-label-2">No devices match this filter.</p>
      ) : (
        <ul
          aria-label="Devices"
          // Two columns on wide screens, in rows: the cards side by side start at the same height.
          className="flex flex-col gap-5 xl:grid xl:grid-cols-2 xl:items-start xl:gap-x-7"
        >
          {shown.map((client) => (
            <li key={client.mac}>
              <ClientSection client={client} />
            </li>
          ))}
        </ul>
      )}

      <UsageSection />
    </>
  );
}

/** The empty page says where devices come from. */
function NoDevices({ status }: { status: Status }) {
  const networks = status.guest.enabled ? `${status.hotspot.ssid} or the guest network` : status.hotspot.ssid;

  return (
    <div className="flex flex-col items-center gap-2 px-6 py-8 text-center">
      <Icon name="devices" size={44} className="text-label-3" />
      <p className="text-title3 text-label">No devices connected.</p>
      <p className="max-w-80 text-subhead text-pretty wrap-break-word text-label-2">
        Phones and laptops that join {networks} appear here.
      </p>
    </div>
  );
}

interface FilterChipsProps {
  clients: NetworkClient[];
  value: ClientFilter;
  onChange: (filter: ClientFilter) => void;
}

/** Filters with live counts, e.g. "5 GHz (3)", as capsules that scroll sideways when space runs out. */
function FilterChips({ clients, value, onChange }: FilterChipsProps) {
  const name = useId();

  return (
    // The negative margins and matching padding leave room inside the scroller for focus rings.
    <div
      role="radiogroup"
      aria-label="Filter clients"
      className="no-scrollbar -mx-4 -my-2 flex gap-2 overflow-x-auto px-4 py-2 lg:mx-0 lg:px-0.5"
    >
      {CLIENT_FILTERS.map((filter) => {
        const selected = filter.value === value;
        const count = clients.filter(filter.matches).length;

        return (
          <label
            key={filter.value}
            className={cn(
              'pressable relative flex min-h-8 shrink-0 cursor-pointer items-center rounded-full px-3.5 text-subhead font-semibold lg:min-h-7 lg:px-3',
              'before:absolute before:inset-x-0 before:-inset-y-1.5',
              'has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-tint',
              selected ? 'bg-tint-fill text-white' : 'bg-fill-2 text-label hover:bg-fill',
            )}
          >
            <input
              type="radio"
              name={name}
              value={filter.value}
              checked={selected}
              onChange={() => onChange(filter.value)}
              className="absolute inset-0 size-full cursor-pointer appearance-none opacity-0"
            />
            {filter.label} ({count})
          </label>
        );
      })}
    </div>
  );
}
