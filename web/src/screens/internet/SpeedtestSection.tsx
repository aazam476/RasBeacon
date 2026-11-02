import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { formatAgo, mbpsValue } from '@/lib/format';
import { useAction } from '@/lib/useAction';
import { AlertDialog, ButtonRow, Row, Section, StatRow, useErrorToast } from '@/ui';
import { NEEDS_INTERNET } from '../common/describe';

/**
 * The last speed test and a way to run a new one. It only runs when asked, one at a time: while
 * one runs, the button says so, also after leaving the page and coming back. On a metered
 * connection (a phone, a paid plan) it asks first, since a test downloads a lot.
 */
export function SpeedtestSection({ status }: { status: Status }) {
  const { runSpeedtest } = useDevice();
  const running = useAction(runSpeedtest, useErrorToast());
  const [confirming, setConfirming] = useState(false);
  const metered = status.hotspot.metered;
  const { provider, last } = status.speedtest;
  const footer = `Uses ${provider} Speedtest. A test uses the internet connection for about 20 seconds.`;

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
        onClick={() => (metered ? setConfirming(true) : void running.run())}
        // Also while a test started earlier (before leaving this page) or elsewhere still runs.
        busy={status.speedtest.running || running.pending}
        busyLabel="Running…"
        disabled={!status.uplink.connected}
        note={!status.uplink.connected && NEEDS_INTERNET}
      >
        Run speed test
      </ButtonRow>
      {confirming && (
        <AlertDialog
          title="Run a speed test?"
          confirmLabel="Run test"
          busyLabel="Starting…"
          // The test runs on without the dialog, which closes at once.
          onConfirm={async () => void running.run()}
          onClose={() => setConfirming(false)}
        >
          <p>This connection is metered. A test can use several hundred MB of data.</p>
        </AlertDialog>
      )}
    </Section>
  );
}
