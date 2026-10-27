import { useState } from 'react';
import { useDevice } from '@/data/device';
import { cn } from '@/lib/cn';
import { Badge, BreakableText, ButtonRow, EditButton, RenameSheet, Row, SheetRow, Switch } from '@/ui';
import { clientDetails, clientName, networkName, type NetworkClient } from '../common/clientDisplay';
import { FixedAddressSheet } from './FixedAddressSheet';

/**
 * One device as a group: its name (renamed with the pencil beside it) and network with the
 * switch for its internet access, its readings, its fixed address (hotspot devices), and
 * disconnecting it (or, while paused, flushing its lease). A device
 * paused by the data limit stays paused until the counts are reset or the limit is raised.
 */
export function ClientSection({ client }: { client: NetworkClient }) {
  const { setClientInternetAccess, disconnectClient, flushClientLease, renameClient, resetWelcome } = useDevice();
  const [renaming, setRenaming] = useState(false);
  const [fixingAddress, setFixingAddress] = useState(false);
  const paused = !client.internetAccess;
  const name = clientName(client);

  return (
    <section className="overflow-hidden rounded-group bg-cell">
      <div className="group row">
        <span className="min-w-0 flex-1">
          {/* Phones have no hover tooltips, so long names wrap instead of being cut off. */}
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h2 className={cn('min-w-0 text-headline wrap-break-word', paused ? 'text-label-2' : 'text-label')}>
              <BreakableText>{name}</BreakableText>
            </h2>
            {/* The tag comes right after the name, so its place doesn't depend on the pencil,
                which is hidden until hovered with a mouse. */}
            {client.isThisDevice && <Badge tone="tint">This device</Badge>}
            <EditButton label={`Rename ${name}`} onClick={() => setRenaming(true)} className="-my-1.5" />
          </span>
          <span className="mt-0.5 block text-subhead text-label-2">
            {networkName(client)}
            {paused && (
              <span className="text-red">
                {' '}
                · {client.limitReached ? 'Data limit reached' : 'Internet access paused'}
              </span>
            )}
          </span>
        </span>
        <Switch
          checked={client.internetAccess}
          onChange={(allowed) => setClientInternetAccess(client.mac, allowed)}
          ariaLabel={`Internet access for ${name}`}
          disabled={client.limitReached}
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
      {/* Guests' addresses come from their own network and aren't kept. */}
      {client.network === 'hotspot' && (
        <SheetRow title="Fixed address" value={client.fixedAddress ?? 'Off'} onClick={() => setFixingAddress(true)} />
      )}
      {/* The welcome page (your own captive portal), on networks that have one. */}
      {client.welcome && (
        <Row title="Welcome page" value={client.welcome === 'accepted' ? 'Accepted' : 'Not accepted yet'} />
      )}
      {client.welcome === 'accepted' && (
        <ButtonRow key="ask-again" action={() => resetWelcome(client.mac)} busyLabel="Asking…">
          Ask again
        </ButtonRow>
      )}
      {paused ? (
        // The keys give each button its own request state.
        <ButtonRow key="flush" tone="danger" action={() => flushClientLease(client.mac)} busyLabel="Flushing…">
          Flush lease
        </ButtonRow>
      ) : (
        // A device on the cable can't be sent off like a Wi-Fi device.
        !client.wired && (
          <ButtonRow key="disconnect" action={() => disconnectClient(client.mac)} busyLabel="Disconnecting…">
            Disconnect
          </ButtonRow>
        )
      )}
      {fixingAddress && <FixedAddressSheet client={client} onClose={() => setFixingAddress(false)} />}
      {renaming && (
        <RenameSheet
          title="Rename device"
          label="Name"
          initial={client.name ?? client.hostname ?? ''}
          placeholder={client.hostname || 'Kitchen tablet'}
          maxLength={40}
          allowEmpty
          footer={
            <>
              Kept for <span className="font-mono wrap-anywhere">{client.mac}</span>, even after it leaves and comes
              back. Leave it empty to use the name the device sends.
            </>
          }
          onSave={(newName) => renameClient(client.mac, newName)}
          onClose={() => setRenaming(false)}
        />
      )}
    </section>
  );
}
