import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status, UpdateReflash } from '@/data/types';
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
import { BackupSheet } from './BackupSection';

const BOARDS: Record<string, string> = {
  rpi64: 'Raspberry Pi 4 and 5',
  rpi3: 'Raspberry Pi Zero 2 W, 3B and 3B+',
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
      {update.reflash ? (
        <ReflashNotice reflash={update.reflash} running={version} hasPlugins={status.plugins.length > 0} />
      ) : (
        update.latestVersion && (
          <Notice
            tone="info"
            icon="system_update_alt"
            title={`Version ${update.latestVersion} is available`}
            actions={update.latestUrl && <ReleaseLink url={update.latestUrl} />}
          >
            <p>Download its .rbupdate file for your board, then install it below.</p>
          </Notice>
        )
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

      {update.singleSystem ? (
        <Notice title="New versions are flashed">
          <p>
            On the Pi Zero 2 W, 3B and 3B+, RasBeacon has one copy of the system, so it can&apos;t install update files.
            To move to a new version, back up your settings below, flash the new image, set it up, and restore the
            backup.
          </p>
        </Notice>
      ) : (
        !update.canInstall && (
          <Notice tone="warning" title="This image can't install updates">
            <p>It has no update signing key, so it can&apos;t install update files. Flash new versions instead.</p>
          </Notice>
        )
      )}
    </>
  );
}

/** A link to a release's page on GitHub, where its files are. */
function ReleaseLink({ url }: { url: string }) {
  return (
    <a href={url} target="_blank" rel="noreferrer" className={buttonClasses('secondary', 'sm')}>
      Release page
      <Icon name="open_in_new" size={16} />
    </a>
  );
}

/**
 * A new version that can't be installed over this one (it changed too much): the steps to it,
 * with the backup one a press away.
 */
function ReflashNotice({
  reflash,
  running,
  hasPlugins,
}: {
  reflash: UpdateReflash;
  running: string;
  hasPlugins: boolean;
}) {
  const [backingUp, setBackingUp] = useState(false);

  return (
    <Notice
      tone="info"
      icon="system_update_alt"
      title={`Version ${reflash.version} needs a fresh install`}
      actions={
        <>
          <Button variant="secondary" size="sm" icon="download" onClick={() => setBackingUp(true)}>
            Back up settings
          </Button>
          {reflash.url && <ReleaseLink url={reflash.url} />}
        </>
      }
    >
      <p>
        It changed too much to be installed over {running}: only {reflash.minFromVersion} or newer can install it. Your
        settings come along in three steps:
      </p>
      <ol className="flex list-decimal flex-col gap-0.5 pl-5">
        <li>
          Back up your settings{hasPlugins && <> (turn on Include plugins to keep them too)</>}, and keep the file and
          its password.
        </li>
        <li>Flash the {reflash.version} image for your board onto the SD card, with Raspberry Pi Imager.</li>
        <li>Set RasBeacon up, then restore the backup under System, Backup.</li>
      </ol>
      {backingUp && <BackupSheet hasPlugins={hasPlugins} onClose={() => setBackingUp(false)} />}
    </Notice>
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
