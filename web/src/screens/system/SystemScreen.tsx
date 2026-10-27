import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { formatDuration } from '@/lib/format';
import { Loaded } from '@/shell/Loaded';
import { useState } from 'react';
import { EditButton, Icon, Meter, RenameSheet, Row, Section } from '@/ui';
import { getResourceMetrics } from '../common/resourceMetrics';
import { AdminPasswordSection } from './AdminPasswordSection';
import { BackupSection } from './BackupSection';
import { FactoryResetSection, RebootSection, ShutdownSection } from './RestartSections';
import { RadioRolesNotice, RadiosSection } from './RadiosSection';
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
          <RadioRolesNotice secondsLeft={status.radioRolesPending} />
          <SoftwareSection status={status} />
          <HardwareSection status={status} />
          <StorageSection storage={status.storage} />
          <SwapSection />
          <AboutSection status={status} />
          <RadiosSection status={status} />
          <DiagnosticsSection />
          <AdminPasswordSection />
          <BackupSection />
          <RebootSection />
          <ShutdownSection />
          <FactoryResetSection />
        </>
      )}
    </Loaded>
  );
}

function HardwareSection({ status }: { status: Status }) {
  const { system } = status;

  return (
    <Section title="Hardware">
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
  const { saveHostname } = useDevice();
  const { device } = status;
  const [renaming, setRenaming] = useState(false);

  return (
    <Section title="About">
      <Row
        className="group"
        // The pencil sits by the title, so the name lines up on the right with the other values.
        title={
          <span className="inline-flex items-center gap-1">
            Name
            <EditButton label="Rename RasBeacon" onClick={() => setRenaming(true)} className="-my-1.5" />
          </span>
        }
        value={device.hostname}
      />
      <Row title="Board" value={status.boardModel} />
      <Row title="Model" value={device.modelSpec} />
      <Row title="Device ID" value={status.deviceId} />
      <Row title="Kernel" value={device.kernel} />
      <Row title="Starts from" value={device.bootDevice} />
      <Row title="Up for" value={formatDuration(status.uptimeSeconds)} />
      {renaming && (
        <RenameSheet
          title="Rename RasBeacon"
          label="Name"
          initial={device.hostname.replace(/\.lan$/, '')}
          placeholder="rasbeacon"
          maxLength={63}
          errorKey="hostname"
          busyLabel="Restarting…"
          plain
          check={checkHostname}
          footer="Devices reach RasBeacon at this name followed by .lan. RasBeacon restarts to use it, so devices disconnect for about a minute."
          onSave={saveHostname}
          onClose={() => setRenaming(false)}
        />
      )}
    </Section>
  );
}

/** A text report to attach to a GitHub issue: a plain file download. */
function DiagnosticsSection() {
  const { diagnosticsUrl } = useDevice();

  return (
    <Section footer="Attach this report when you open an issue on GitHub. Passwords are removed and device addresses are partly hidden.">
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

/** RasBeacon's rules for its name (internal/config ValidateHostname), checked while typing. */
function checkHostname(name: string): string | undefined {
  const lower = name.toLowerCase();
  if (lower.length > 63) return 'Use at most 63 characters.';
  if (!/^[a-z0-9-]+$/.test(lower)) return 'Use only lowercase letters, digits and hyphens.';
  if (lower.startsWith('-') || lower.endsWith('-')) return "The name can't start or end with a hyphen.";
  return undefined;
}
