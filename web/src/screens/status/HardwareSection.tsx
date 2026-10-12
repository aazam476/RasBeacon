import type { Status } from '@/data/types';
import { formatDuration } from '@/lib/format';
import { Meter, Row, Section } from '@/ui';
import { getResourceMetrics } from '../common/resourceMetrics';

/** CPU, memory, swap and temperature with their meters, and how long RasBeacon has been up. */
export function HardwareSection({ status }: { status: Status }) {
  return (
    <Section title="Hardware">
      {Object.values(getResourceMetrics(status.system)).map((metric) => (
        <Row key={metric.label} title={metric.label} value={metric.value}>
          <Meter percent={metric.percent} tone={metric.tone} label={metric.label} />
        </Row>
      ))}
      <Row title="Up for" value={formatDuration(status.uptimeSeconds)} />
    </Section>
  );
}
