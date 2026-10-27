import type { LANPortStatus } from '@/data/types';
import { Notice, Row, Section } from '@/ui';

/** What a USB Ethernet port is doing, in a few words. */
function portState(port: LANPortStatus): string {
  if (port.connected) return 'For devices';
  if (port.foreignDhcpServer) return 'Not used';
  return port.cablePlugged ? 'Checking…' : 'No cable';
}

/**
 * The USB Ethernet adapters plugged into RasBeacon: extra wired ports for devices, so the
 * Ethernet port can carry the internet while wired devices still connect. Shown only when there
 * is one.
 */
export function LANPortsSection({ ports }: { ports: LANPortStatus[] }) {
  if (ports.length === 0) return null;
  const several = ports.length > 1;
  const foreign = ports.filter((port) => port.foreignDhcpServer);

  return (
    <>
      <Section
        title="USB Ethernet"
        footer="Plug a computer, a game console or a switch into a USB Ethernet adapter to use the hotspot by cable. A cable that leads to another network is never connected."
      >
        {ports.map((port) => (
          <Row
            key={port.name}
            leading={{ icon: 'settings_ethernet' }}
            title={several ? `Adapter ${port.name}` : 'Adapter'}
            value={portState(port)}
          />
        ))}
      </Section>
      {foreign.map((port) => (
        <Notice key={port.name} tone="warning" role="alert" title="This cable seems to lead to a network">
          <p>
            A DHCP server answered on {several ? `adapter ${port.name}` : 'the USB Ethernet adapter'} (
            {port.foreignDhcpServer}), so RasBeacon kept it off your hotspot. USB Ethernet is for your own devices; use
            the Ethernet port to connect to the internet.
          </p>
        </Notice>
      ))}
    </>
  );
}
