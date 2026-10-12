import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { EthernetDiagnostics, EthernetRole, EthernetUplinkStatus } from '@/data/types';
import { AlertDialog, Badge, ButtonRow, Choice, ChoiceList, Notice, Row, Section, type BaseIconName } from '@/ui';

const ROLES: Record<EthernetRole, { title: string; description: string; icon: BaseIconName }> = {
  uplink: {
    title: 'Uplink',
    description: "The cable connects RasBeacon to the internet (for example a hotel room's wall socket).",
    icon: 'public',
  },
  'client-port': {
    title: 'Client port (for your own laptop)',
    description: 'Plug in one of your own devices to use the hotspot by cable.',
    icon: 'laptop_mac',
  },
};

interface EthernetSectionProps {
  role: EthernetRole;
  port?: EthernetUplinkStatus;
  /** Details of the port while the cable carries the internet. */
  diagnostics?: EthernetDiagnostics | null;
}

/**
 * What the Ethernet socket is for, and its details while it carries the internet. RasBeacon has
 * no Wi-Fi/Ethernet switch: as an uplink, a plugged-in cable carries the internet by itself, and
 * Wi-Fi takes over when it is unplugged.
 */
export function EthernetSection({ role, port, diagnostics }: EthernetSectionProps) {
  const { rescanEthernetPort, setEthernetRole } = useDevice();
  const [switchingTo, setSwitchingTo] = useState<EthernetRole | null>(null);

  return (
    <>
      <Section
        title="Ethernet port"
        action={port && (port.cablePlugged ? <Badge tone="green">Cable plugged in</Badge> : <Badge>No cable</Badge>)}
        footer="As an uplink, a plugged-in cable carries the internet by itself; Wi-Fi takes over when it is unplugged."
      >
        <ChoiceList
          ariaLabel="Ethernet port"
          value={role}
          onChange={(next) => {
            if (next !== role) setSwitchingTo(next);
          }}
        >
          {(Object.keys(ROLES) as EthernetRole[]).map((value) => (
            <Choice
              key={value}
              value={value}
              title={ROLES[value].title}
              description={ROLES[value].description}
              icon={ROLES[value].icon}
            />
          ))}
        </ChoiceList>
        <ButtonRow action={rescanEthernetPort} busyLabel="Checking the port…">
          Rescan port
        </ButtonRow>
      </Section>

      {role === 'client-port' && port?.foreignDhcpServer && (
        <Notice tone="warning" role="alert" title="This cable seems to lead to a network">
          <p>
            A DHCP server answered on the client port ({port.foreignDhcpServer}), so the cable probably goes to a
            network, not to your own device. If it should carry the internet, choose Uplink.
          </p>
        </Notice>
      )}

      {diagnostics && (
        <Section
          title="Port details"
          footer={`${diagnostics.portDescription} Checked every ${diagnostics.pollingInterval}.`}
        >
          <Row title="Carrier" value={diagnostics.carrierState} />
          <Row title="Controller" value={diagnostics.controller} />
          <Row title="MTU" value={diagnostics.mtu} />
          <Row title="Packets" value={diagnostics.packetStatistics} />
          <Row title="DNS servers" value={diagnostics.dnsServers} />
          <Row title="DHCP lease" value={diagnostics.dhcpLease} />
        </Section>
      )}

      {switchingTo && (
        <AlertDialog
          title={switchingTo === 'client-port' ? 'Use the port for your own device?' : 'Use the port for the internet?'}
          confirmLabel="Switch now"
          busyLabel="Switching…"
          onConfirm={() => setEthernetRole(switchingTo)}
          onClose={() => setSwitchingTo(null)}
        >
          <p>
            {switchingTo === 'client-port'
              ? 'If the cable carries the internet now, RasBeacon switches to Wi-Fi: expect a brief connection drop.'
              : 'A device plugged into the port loses its connection, and RasBeacon uses the cable for the internet.'}
          </p>
        </AlertDialog>
      )}
    </>
  );
}
