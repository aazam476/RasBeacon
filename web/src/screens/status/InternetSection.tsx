import type { Speedtest, Status } from '@/data/types';
import { formatAgo, formatBand, formatSignal, mbpsValue } from '@/lib/format';
import { Badge, BreakableText, LinkRow, Row, Section, SignalIcon, StatRow, type RowLeading } from '@/ui';
import { describeEthernet, describeIpv6, describeUplink, describeWiFiState, uplinkBadge } from '../common/describe';

/** How RasBeacon reaches the internet: the connection, its addresses, and the last speed test. */
export function InternetSection({ status }: { status: Status }) {
  const { uplink, vpn, speedtest } = status;
  const { wifi, ethernet } = uplink;
  const onWifi = uplink.connected && uplink.kind === 'wifi';
  const onEthernet = uplink.connected && uplink.kind === 'ethernet';
  const badge = uplinkBadge(uplink);

  let leading: RowLeading = { tile: 'public', color: 'gray' };
  let detail: string | undefined;
  if (onWifi) {
    leading = { tile: 'wifi', color: 'blue' };
    detail = `${formatSignal(wifi.signalDbm)}, ${formatBand(wifi.band)}`;
  } else if (onEthernet) {
    leading = { tile: 'settings_ethernet', color: 'green' };
    detail = describeEthernet(ethernet);
  } else if (!wifi.available) {
    detail = 'No radio free to join networks';
  } else if (wifi.state !== 'login-failed') {
    detail = describeWiFiState(wifi.state);
  }

  return (
    <Section
      title="Internet"
      action={
        <Badge tone={badge.tone} pulse={badge.pulse}>
          {badge.label}
        </Badge>
      }
    >
      <Row
        leading={leading}
        title={<BreakableText>{describeUplink(uplink)}</BreakableText>}
        subtitle={detail}
        trailing={onWifi ? <SignalIcon dbm={wifi.signalDbm} /> : undefined}
      />
      {uplink.connected && (
        <>
          <Row title="Address on that network" value={uplink.address || 'waiting for address'} />
          <Row title="Gateway" value={uplink.gateway || '–'} />
          <Row title="IPv6 for your devices" value={describeIpv6(uplink, vpn)} />
        </>
      )}
      {!onEthernet && ethernet.present && <Row title="Ethernet" value={describeEthernet(ethernet)} />}
      <SpeedtestRows speedtest={speedtest} />
      <LinkRow to="/internet" title="Manage networks" />
    </Section>
  );
}

/** The last speed test's readings, when there has been one. */
function SpeedtestRows({ speedtest }: { speedtest: Speedtest }) {
  const { last } = speedtest;
  if (!last) return null;

  return (
    <>
      <Row title="Last speed test" value={`${formatAgo(last.ageSeconds)}, ${speedtest.provider}`} />
      <StatRow
        stats={[
          { label: 'Download', value: mbpsValue(last.downloadMbps), unit: 'Mbps', highlight: true },
          { label: 'Upload', value: mbpsValue(last.uploadMbps), unit: 'Mbps' },
          { label: 'Ping', value: String(Math.round(last.pingMs)), unit: 'ms' },
        ]}
      />
    </>
  );
}
