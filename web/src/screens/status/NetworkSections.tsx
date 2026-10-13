import type { Client, GuestStatus, HotspotStatus } from '@/data/types';
import { formatBand, formatDuration } from '@/lib/format';
import { Badge, BreakableText, Icon, LinkRow, Row, Section, SignalIcon } from '@/ui';
import { clientName } from '../common/clientDisplay';
import { describeMetered, describeSecurity } from '../common/describe';

/** RasBeacon's own Wi-Fi network as it runs now. */
export function HotspotSection({ hotspot }: { hotspot: HotspotStatus }) {
  return (
    <Section
      title="Hotspot"
      action={hotspot.running ? <Badge tone="green">Running</Badge> : <Badge tone="red">Not running</Badge>}
    >
      <Row
        leading={{ tile: 'wifi', color: 'green' }}
        title="Network name"
        value={<BreakableText>{hotspot.ssid}</BreakableText>}
      />
      <Row title="Band and channel" value={`${formatBand(hotspot.band)}, channel ${hotspot.channel}`} />
      <Row title="Security" value={describeSecurity(hotspot.security)} />
      <Row title="Address" value={hotspot.lanAddress} />
      <Row title="Metered connection" value={describeMetered(hotspot)} />
      <LinkRow to="/hotspot" title="Hotspot settings" />
    </Section>
  );
}

/** The guest network: off, or its name, whether it runs and how many guests use it. */
export function GuestSection({ guest }: { guest: GuestStatus }) {
  let badge = <Badge>Off</Badge>;
  if (guest.enabled) badge = guest.running ? <Badge tone="green">On</Badge> : <Badge tone="orange">Not running</Badge>;

  return (
    <Section title="Guest network" action={badge}>
      {guest.enabled ? (
        <>
          <Row
            leading={{ tile: 'group', color: 'orange' }}
            title="Network name"
            value={<BreakableText>{guest.ssid}</BreakableText>}
          />
          <Row title="Guests connected" value={guest.clients.length} />
          <LinkRow to="/guests" title="Guest network settings" />
        </>
      ) : (
        <LinkRow to="/guests" leading={{ tile: 'group', color: 'orange' }} title="Set up a guest network" />
      )}
    </Section>
  );
}

/** How many devices the Status page lists before linking to the rest. */
const LISTED = 5;

/**
 * Devices on the hotspot at a glance; the Clients page has the details and controls. Guests
 * are counted under the list, so the numbers add up to the total at the top of the page.
 */
export function DevicesSection({ clients, guests }: { clients: Client[]; guests: number }) {
  const listed = clients.length > LISTED + 1 ? clients.slice(0, LISTED) : clients;

  return (
    <Section
      title={clients.length > 0 ? `Connected devices (${clients.length})` : 'Connected devices'}
      footer={guests > 0 ? `Plus ${guests} on the guest network.` : undefined}
    >
      {clients.length === 0 && <Row title={<span className="text-label-2">No devices connected.</span>} />}
      {listed.map((client) => (
        <Row
          key={client.mac}
          title={<BreakableText>{clientName(client)}</BreakableText>}
          subtitle={`${client.ip || 'waiting for address'} · ${formatDuration(client.connectedSeconds)}`}
          trailing={
            client.wired ? (
              <span role="img" aria-label="Ethernet cable" className="text-label-2">
                <Icon name="settings_ethernet" />
              </span>
            ) : (
              <SignalIcon dbm={client.signalDbm} />
            )
          }
        />
      ))}
      <LinkRow
        to="/clients"
        title={listed.length < clients.length ? `All ${clients.length} devices` : 'Manage devices'}
      />
    </Section>
  );
}
