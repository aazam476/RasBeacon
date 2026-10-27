import { useDevice } from '@/data/device';
import type { Status, UplinkStatus } from '@/data/types';
import { bandOfFrequency, formatBand, formatBytes, formatSignal, keepTogether } from '@/lib/format';
import { phoneInUse, phoneName } from '../common/describe';
import {
  Badge,
  BreakableText,
  ButtonRow,
  Notice,
  Row,
  Section,
  SignalIcon,
  type BaseIconName,
  type NoticeTone,
} from '@/ui';

/** The connection carrying the internet now (a Wi-Fi network or the cable), or why there is none. */
export function ConnectionSection({ status }: { status: Status }) {
  const { disconnectUplink, renewDhcpLease } = useDevice();
  const { uplink } = status;
  const { wifi, ethernet } = uplink;
  if (!uplink.connected) return <UplinkProblem uplink={uplink} />;

  const onWifi = uplink.kind === 'wifi';
  const phone = phoneInUse(uplink);

  return (
    <Section title="Connection" action={<Badge tone="green">Online</Badge>}>
      {uplink.kind === 'phone' ? (
        <Row
          leading={{ tile: 'usb', color: 'orange' }}
          title={<BreakableText>{phoneName(phone)}</BreakableText>}
          subtitle={`Over USB (${uplink.interface})`}
        />
      ) : onWifi ? (
        <Row
          leading={{ tile: 'wifi', color: 'blue' }}
          title={<BreakableText>{wifi.ssid}</BreakableText>}
          subtitle={`${formatSignal(wifi.signalDbm)}, ${formatBand(wifi.band)}`}
          trailing={<SignalIcon dbm={wifi.signalDbm} />}
        />
      ) : (
        <Row
          leading={{ tile: 'settings_ethernet', color: 'green' }}
          title="Ethernet"
          subtitle={`Cable plugged in (${uplink.interface})`}
          trailing={
            ethernet.linkSpeed && <span className="text-body text-label-2">{keepTogether(ethernet.linkSpeed)}</span>
          }
        />
      )}
      <Row title="IP address" value={uplink.address || 'waiting for address'} />
      <Row title="Gateway" value={uplink.gateway || '–'} />
      {uplink.subnetMask && <Row title="Subnet mask" value={uplink.subnetMask} />}
      {onWifi && wifi.bssid && <Row title="BSSID" value={wifi.bssid} />}
      {onWifi && wifi.channel !== undefined && (
        <Row
          title="Channel"
          value={`${wifi.channel}${wifi.frequencyMhz !== undefined ? ` (${bandOfFrequency(wifi.frequencyMhz)})` : ''}`}
        />
      )}
      {uplink.kind === 'ethernet' && ethernet.duplex && <Row title="Link" value={ethernet.duplex} />}
      {phone && (
        <Row
          title="Data used"
          value={`↓\u00a0${formatBytes(phone.receivedBytes)} ↑\u00a0${formatBytes(phone.sentBytes)}`}
        />
      )}
      {uplink.interfaceMac && <Row title="MAC address" value={uplink.interfaceMac} />}
      <ButtonRow action={renewDhcpLease} busyLabel="Renewing…">
        Renew lease
      </ButtonRow>
      <ButtonRow action={disconnectUplink} tone="danger" busyLabel="Disconnecting…">
        Disconnect
      </ButtonRow>
    </Section>
  );
}

interface Problem {
  tone: NoticeTone;
  icon: BaseIconName;
  title: string;
  text: string;
}

/** Why RasBeacon has no internet right now, and what to do. */
function describeProblem(uplink: UplinkStatus): Problem {
  const { wifi, ethernet } = uplink;
  const cableWaiting = ethernet.present && ethernet.role === 'uplink' && ethernet.cablePlugged;

  if (!wifi.available || wifi.state === 'off') {
    return {
      tone: 'warning',
      icon: 'wifi_off',
      title: 'No Wi-Fi radio free for the internet',
      text: cableWaiting
        ? 'The Ethernet cable is plugged in, but no internet came through it yet.'
        : ethernet.present
          ? 'The only radio runs the hotspot. Plug in an Ethernet cable, or a USB Wi-Fi adapter for the hotspot.'
          : 'The only radio runs the hotspot. Plug in a USB Wi-Fi adapter for the hotspot.',
    };
  }
  switch (wifi.state) {
    case 'connecting':
      return {
        tone: 'neutral',
        icon: 'sync',
        title: 'Connecting…',
        text: wifi.ssid ? `RasBeacon is joining "${wifi.ssid}".` : 'RasBeacon is joining a saved network.',
      };
    case 'login-failed':
      return {
        tone: 'danger',
        icon: 'error',
        title: 'The network rejected the login',
        text: 'Check the username, password, certificate and server name of the network in the list below.',
      };
    case 'not-configured':
      return {
        tone: 'warning',
        icon: 'wifi_find',
        title: 'No networks saved',
        text: 'Add a network below, or plug in an Ethernet cable.',
      };
    default:
      return {
        tone: 'neutral',
        icon: 'wifi_find',
        title: 'Looking for your saved networks',
        text: 'None of them is in range yet. RasBeacon keeps looking; you can add a network below.',
      };
  }
}

function UplinkProblem({ uplink }: { uplink: UplinkStatus }) {
  const problem = describeProblem(uplink);
  const urgent = problem.tone === 'danger' || problem.tone === 'warning';

  return (
    <Notice tone={problem.tone} icon={problem.icon} title={problem.title} role={urgent ? 'alert' : 'status'}>
      <p>{problem.text}</p>
    </Notice>
  );
}
