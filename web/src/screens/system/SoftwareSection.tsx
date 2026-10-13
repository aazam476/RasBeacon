import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status } from '@/data/types';
import { useAction } from '@/lib/useAction';
import {
  ActivityIndicator,
  AlertDialog,
  Button,
  buttonClasses,
  FileField,
  FormError,
  FormSuccess,
  Icon,
  Notice,
  Row,
  Section,
  SwitchRow,
} from '@/ui';

const BOARDS: Record<string, string> = {
  rpi64: 'Raspberry Pi 4 and 5',
  pi4: 'Raspberry Pi 4',
  pi5: 'Raspberry Pi 5',
};

/** The running version, news of a newer one, the result of the last update, and installing one. */
export function SoftwareSection({ status }: { status: Status }) {
  const { setCheckOnline } = useDevice();
  const { update, version } = status;
  const board = BOARDS[update.board] ?? update.board;

  return (
    <>
      {update.installing && (
        <Notice tone="neutral" icon="system_update_alt" role="status" title="Installing an update…">
          <p>RasBeacon restarts when it&apos;s done. Connected devices are offline for about two minutes.</p>
        </Notice>
      )}
      {update.latestVersion && (
        <Notice
          tone="info"
          icon="system_update_alt"
          title={`Version ${update.latestVersion} is available`}
          actions={
            update.latestUrl && (
              <a href={update.latestUrl} target="_blank" rel="noreferrer" className={buttonClasses('secondary', 'sm')}>
                Release page
                <Icon name="open_in_new" size={16} />
              </a>
            )
          }
        >
          <p>Download its .rbupdate file for your board, then install it below.</p>
        </Notice>
      )}

      <Section title="Software">
        <Row
          leading={{ tile: 'settings', color: 'gray' }}
          title={`RasBeacon ${version}`}
          subtitle={board ? `For ${board}` : undefined}
        />
        {update.message && <Row title="Last update" subtitle={update.message} />}
        {update.canCheckOnline && (
          <SwitchRow
            title="Check online for new versions"
            subtitle="Asks GitHub every few hours; never installs anything by itself"
            checked={update.checkOnline}
            onChange={setCheckOnline}
          />
        )}
        {update.canInstall && <InstallUpdate busy={update.installing} />}
      </Section>

      {!update.canInstall && (
        <Notice tone="warning" title="This image can't install updates">
          <p>It has no update signing key, so it can&apos;t install update files. Flash new versions instead.</p>
        </Notice>
      )}
    </>
  );
}

/**
 * Choose a .rbupdate file and install it. Uploading and checking take minutes, so after the
 * confirmation the alert closes and the group shows the progress.
 */
function InstallUpdate({ busy }: { busy: boolean }) {
  const { installUpdate } = useDevice();
  const [file, setFile] = useState<File | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [installed, setInstalled] = useState(false);
  const installing = useAction(async (chosen: File) => {
    await installUpdate(chosen);
    setInstalled(true);
  });

  return (
    <>
      <FileField
        label="Update file"
        labelNote="(.rbupdate, from the Releases page)"
        accept=".rbupdate"
        disabled={installing.pending || busy}
        onChange={(files) => {
          setFile(files[0] ?? null);
          setInstalled(false);
        }}
      />
      {(installing.pending || installing.error || (installed && !installing.pending)) && (
        <div className="row">
          {installing.pending ? (
            <ActivityIndicator
              label="Uploading and checking the update… keep this page open (a few minutes)."
              className="text-subhead text-label"
            />
          ) : installing.error ? (
            <FormError>{installing.error}</FormError>
          ) : (
            <FormSuccess>
              Update installed. RasBeacon is restarting to try it; reconnect in about two minutes.
            </FormSuccess>
          )}
        </div>
      )}
      <div className="row">
        <Button
          icon="system_update_alt"
          loading={installing.pending}
          disabled={!file || busy}
          onClick={() => setConfirming(true)}
        >
          {installing.pending ? 'Installing…' : 'Install update'}
        </Button>
      </div>

      {confirming && file && (
        <AlertDialog
          title="Install the update?"
          confirmLabel="Install"
          busyLabel="Starting…"
          onConfirm={async () => {
            // The group shows the progress; the alert closes at once.
            void installing.run(file);
          }}
          onClose={() => setConfirming(false)}
        >
          <p>
            Install <span className="font-mono wrap-anywhere">{file.name}</span>? RasBeacon restarts afterwards;
            connected devices are offline for about two minutes. If the new version doesn&apos;t start correctly,
            RasBeacon goes back to this one by itself.
          </p>
        </AlertDialog>
      )}
    </>
  );
}
