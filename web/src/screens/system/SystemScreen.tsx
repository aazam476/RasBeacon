import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { formatDuration } from '@/lib/format';
import { Loaded } from '@/shell/Loaded';
import { Badge, Icon, Meter, Row, Section, type Tone } from '@/ui';
import { getResourceMetrics } from '../common/resourceMetrics';
import { BackupSection } from './BackupSection';
import { FactoryResetSection, RebootSection } from './RestartSections';
import { SoftwareSection } from './SoftwareSection';
import { StorageSection } from './StorageSection';
import { SwapSection } from './SwapSection';

/** Software updates, hardware health and identity, diagnostics, restarting and factory reset. */
export function SystemScreen() {
  const { status, refreshStatus } = useDevice();

  return (
    <Loaded data={status} what="the system status" onRetry={refreshStatus}>
      {(status) => (
        <>
          <SoftwareSection status={status} />
          <HardwareSection status={status} />
          <StorageSection storage={status.storage} />
          <SwapSection />
          <AboutSection status={status} />
          <RadiosSection status={status} />
          <DiagnosticsSection />
          <BackupSection />
          <RebootSection />
          <FactoryResetSection />
        </>
      )}
    </Loaded>
  );
}

function HardwareSection({ status }: { status: Status }) {
  const { system } = status;

  return (
    <Section
      title="Hardware"
      footer={`Cooling: ${system.cooling}. Sensor: ${system.sensorStatus}. Slowed down for heat: ${system.throttling}.`}
    >
      {Object.values(getResourceMetrics(system)).map((metric) => (
        <Row
          key={metric.label}
          leading={{ icon: metric.icon }}
          title={metric.label}
          subtitle={metric.detail}
          value={metric.value}
        >
          <Meter percent={metric.percent} tone={metric.tone} label={metric.label} />
        </Row>
      ))}
    </Section>
  );
}

function AboutSection({ status }: { status: Status }) {
  const { device } = status;

  return (
    <Section title="About">
      <Row title="Name" value={device.hostname} />
      <Row title="RasBeacon version" value={status.version} />
      <Row title="Board" value={status.boardModel} />
      <Row title="Model" value={device.modelSpec} />
      <Row title="Device ID" value={status.deviceId} />
      <Row title="Kernel" value={device.kernel} />
      <Row title="Starts from" value={device.bootDevice} />
      <Row title="Up for" value={formatDuration(status.uptimeSeconds)} />
    </Section>
  );
}

const ROLES: Record<Status['radios'][number]['role'], { label: string; tone: Tone }> = {
  hotspot: { label: 'Hotspot', tone: 'green' },
  uplink: { label: 'Internet', tone: 'tint' },
  unused: { label: 'Unused', tone: 'gray' },
};

/** The Wi-Fi radios RasBeacon found and what each one does. */
function RadiosSection({ status }: { status: Status }) {
  return (
    <Section title="Wi-Fi radios">
      {status.radios.map((radio) => (
        <Row
          key={radio.interface}
          leading={{ icon: 'wifi' }}
          title={<span className="font-mono">{radio.interface}</span>}
          subtitle={`${radio.driver} · ${radio.isUsb ? `USB ${radio.usbId}` : 'built-in'} · ${
            radio.maxAps > 1 ? `can broadcast ${radio.maxAps} networks` : 'one network at a time'
          }`}
          trailing={<Badge tone={ROLES[radio.role].tone}>{ROLES[radio.role].label}</Badge>}
        />
      ))}
    </Section>
  );
}

/** A text report to attach when asking for help: a plain file download. */
function DiagnosticsSection() {
  const { diagnosticsUrl } = useDevice();

  return (
    <Section footer="A text report to attach when asking for help. Passwords are removed and device addresses are partly hidden.">
      <a
        href={diagnosticsUrl}
        download="rasbeacon-diagnostics.txt"
        className="row row-pressable text-body text-tint no-underline focus-visible:-outline-offset-2"
      >
        <span className="row-tile grid w-[1.8125rem] shrink-0 place-items-center lg:w-6">
          <Icon name="download" size={22} className="lg:size-[1.125rem]" />
        </span>
        <span className="flex-1">Download diagnostics</span>
      </a>
    </Section>
  );
}
