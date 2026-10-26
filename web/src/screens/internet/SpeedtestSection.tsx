import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { formatAgo, mbpsValue } from '@/lib/format';
import { ButtonRow, Row, Section, StatRow } from '@/ui';
import { NEEDS_INTERNET } from '../common/describe';

/**
 * The last speed test and a way to run a new one. It only runs when asked, one at a time: while
 * one runs, the button says so, also after leaving the page and coming back.
 */
export function SpeedtestSection({ status }: { status: Status }) {
  const { runSpeedtest } = useDevice();
  const { provider, last } = status.speedtest;
  // On a metered connection (a phone's hotspot, a paid plan), the data a test downloads counts.
  const footer = [
    `Uses ${provider} Speedtest. A test uses the internet connection for about 20 seconds.`,
    status.hotspot.metered && 'A test can use several hundred MB of data.',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <Section title="Speed test" footer={footer}>
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
        // Also while a test started earlier (before leaving this page) or elsewhere still runs.
        busy={status.speedtest.running}
        busyLabel="Running…"
        disabled={!status.uplink.connected}
        note={!status.uplink.connected && NEEDS_INTERNET}
      >
        Run speed test
      </ButtonRow>
    </Section>
  );
}
