import type { Throughput } from '@/data/types';
import { formatBytes, formatMbps, mbpsValue } from '@/lib/format';
import { Row, Section, StatRow } from '@/ui';

/** Live download and upload through RasBeacon, against this session's peaks. */
export function TrafficSection({ throughput }: { throughput: Throughput }) {
  return (
    <Section title="Traffic">
      <StatRow
        stats={[
          {
            label: 'Download',
            value: mbpsValue(throughput.downMbps),
            unit: 'Mbps',
            caption: `Peak ${formatMbps(throughput.downPeakMbps)}`,
            highlight: true,
          },
          {
            label: 'Upload',
            value: mbpsValue(throughput.upMbps),
            unit: 'Mbps',
            caption: `Peak ${formatMbps(throughput.upPeakMbps)}`,
          },
        ]}
      />
      <Row title="Routed this session" value={formatBytes(throughput.sessionBytes)} />
      <Row title="Highest combined rate" value={formatMbps(throughput.peakMbps)} />
    </Section>
  );
}
