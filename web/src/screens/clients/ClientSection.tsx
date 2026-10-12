import { useDevice } from '@/data/device';
import { cn } from '@/lib/cn';
import { Badge, BreakableText, ButtonRow, Switch } from '@/ui';
import { clientDetails, clientName, networkName, type NetworkClient } from '../common/clientDisplay';

/**
 * One device as a group: its name and network with the switch for its internet access, its
 * readings, and disconnecting it (or, while paused, flushing its lease).
 */
export function ClientSection({ client }: { client: NetworkClient }) {
  const { setClientInternetAccess, disconnectClient, flushClientLease } = useDevice();
  const paused = !client.internetAccess;
  const name = clientName(client);

  return (
    <section className="overflow-hidden rounded-group bg-cell">
      <div className="row">
        <span className="min-w-0 flex-1">
          {/* Phones have no hover tooltips, so long names wrap instead of being cut off. */}
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h2 className={cn('min-w-0 text-headline wrap-break-word', paused ? 'text-label-2' : 'text-label')}>
              <BreakableText>{name}</BreakableText>
            </h2>
            {client.isThisDevice && <Badge tone="tint">This device</Badge>}
          </span>
          <span className="mt-0.5 block text-subhead text-label-2">
            {networkName(client)}
            {paused && <span className="text-red"> · Internet access paused</span>}
          </span>
        </span>
        <Switch
          checked={client.internetAccess}
          onChange={(allowed) => setClientInternetAccess(client.mac, allowed)}
          ariaLabel={`Internet access for ${name}`}
        />
      </div>
      <dl className="row grid grid-cols-2 items-start gap-x-4 gap-y-2.5 py-3 lg:grid-cols-[repeat(auto-fit,minmax(9rem,1fr))]">
        {clientDetails(client).map((detail) => (
          // The last one (the MAC address) takes a whole line when it would sit alone, so it never wraps.
          <div key={detail.label} className="min-w-0 last:odd:col-span-2">
            <dt className="text-footnote text-label-2">{detail.label}</dt>
            <dd className={cn('selectable text-subhead wrap-break-word', detail.alert ? 'text-red' : 'text-label')}>
              {detail.value}
            </dd>
          </div>
        ))}
      </dl>
      {paused ? (
        // The keys give each button its own request state.
        <ButtonRow key="flush" tone="danger" action={() => flushClientLease(client.mac)} busyLabel="Flushing…">
          Flush lease
        </ButtonRow>
      ) : (
        <ButtonRow key="disconnect" action={() => disconnectClient(client.mac)} busyLabel="Disconnecting…">
          Disconnect
        </ButtonRow>
      )}
    </section>
  );
}
