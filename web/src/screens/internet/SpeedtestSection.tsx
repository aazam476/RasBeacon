import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { formatAgo, mbpsValue } from '@/lib/format';
import { ButtonRow, Row, Section, StatRow } from '@/ui';

/** The last speed test and a way to run a new one. It only runs when asked. */
export function SpeedtestSection({ status }: { status: Status }) {
  const { runSpeedtest } = useDevice();
  const { provider, last } = status.speedtest;

  return (
    <Section
      title="Speed test"
      footer={`Uses ${provider} Speedtest. A test uses the internet connection for about 20 seconds.`}
    >
      {last ? (
        <>
          <StatRow
            stats={[
              { label: 'Download', value: mbpsValue(last.downloadMbps), unit: 'Mbps', highlight: true },
              { label: 'Upload', value: mbpsValue(last.uploadMbps), unit: 'Mbps' },
              { label: 'Ping', value: String(Math.round(last.pingMs)), unit: 'ms' },
            ]}
          />
          <Row title="Tested" value={formatAgo(last.ageSeconds)} />
          {last.server && <Row title="Server" value={last.server} />}
        </>
      ) : (
        <Row title={<span className="text-label-2">No speed test yet. Run one to measure this connection.</span>} />
      )}
      <ButtonRow
        leading={{ icon: 'speed', className: 'text-tint' }}
        action={runSpeedtest}
        busyLabel="Running…"
        disabled={!status.uplink.connected}
      >
        Run speed test
      </ButtonRow>
    </Section>
  );
}
