import { useRef, useState, type ReactNode } from 'react';
import { useDevice } from '@/data/device';
import { errorText } from '@/data/errors';
import type { Status, UpdateReflash, UpdateSettings } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { useAction } from '@/lib/useAction';
import {
  ActionButton,
  ActivityIndicator,
  AlertDialog,
  Button,
  buttonClasses,
  FormError,
  FormSuccess,
  Icon,
  Notice,
  Row,
  Section,
  SelectField,
  TextField,
} from '@/ui';
import { NEEDS_INTERNET } from '../common/describe';
import { BackupSheet } from './BackupSection';

const BOARDS: Record<string, string> = {
  rpi64: 'Raspberry Pi 4 and 5',
  rpi3: 'Raspberry Pi Zero 2 W, 3B and 3B+',
};

/** The running version, news of a newer one, the result of the last update, and installing one. */
export function SoftwareSection({ status }: { status: Status }) {
  const { update, version } = status;
  const board = BOARDS[update.board] ?? update.board;

  return (
    <>
      {update.installing && (
        <Notice tone="neutral" icon="system_update_alt" role="status" title="Installing an update…">
          <p>RasBeacon restarts when it&apos;s done. Connected devices are offline for about two minutes.</p>
        </Notice>
      )}
      {update.restartPending && <RestartPendingNotice message={update.message} />}
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
            <p>
              {update.downloadSize
                ? 'Press Update now below: RasBeacon downloads and installs it by itself.'
                : 'Download its .rbupdate file for your board, then press Update from a file below.'}
            </p>
            {update.newerVersion && (
              <p>
                {update.newerVersion} is out too, but can&apos;t be installed over this RasBeacon yet. Install this one
                first: RasBeacon then offers the next step.
              </p>
            )}
          </Notice>
        )
      )}

      <Section title="Software" footer={lastCheckedText(update.lastChecked)}>
        <Row
          leading={{ tile: 'settings', color: 'gray' }}
          title={`RasBeacon ${version}`}
          subtitle={board ? `For ${board}` : undefined}
        />
        {update.message && !update.restartPending && !update.installing && (
          <Row title="Last update" subtitle={update.message} />
        )}
        {update.canCheckOnline && <CheckScheduleRow checkHours={update.checkHours} />}
        {(update.canCheckOnline || update.canInstall) && <UpdateActions status={status} />}
      </Section>
      {update.canCheckOnline && <NotificationsSection />}

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

/** "Last checked Oct 19, 7:13 PM.", or nothing before the first check. */
function lastCheckedText(lastChecked: string | undefined): string | undefined {
  const time = lastChecked ? Date.parse(lastChecked) : NaN;
  if (Number.isNaN(time)) return undefined;
  const when = new Date(time).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
  return `Last checked ${when}.`;
}

const CHECK_OPTIONS = [
  { value: '0', label: 'Manually' },
  { value: '1', label: 'Every hour' },
  { value: '6', label: 'Every 6 hours' },
  { value: '24', label: 'Every day' },
  { value: '168', label: 'Every week' },
];

/**
 * How often RasBeacon asks GitHub for a new version; it saves as soon as it changes. RasBeacon
 * only tells you; installing stays yours to do.
 */
function CheckScheduleRow({ checkHours }: { checkHours: number }) {
  const { updateSettings, saveUpdateSettings } = useDevice();
  const [error, setError] = useState<string | null>(null);

  return (
    <SelectField
      label="Check for new versions"
      options={CHECK_OPTIONS}
      value={String(checkHours)}
      disabled={updateSettings.data === undefined}
      error={error ?? undefined}
      onChange={(hours) => {
        setError(null);
        saveUpdateSettings({ notifyUrl: updateSettings.data?.notifyUrl ?? '', checkHours: Number(hours) }).catch(
          (problem: unknown) => setError(errorText(problem)),
        );
      }}
    />
  );
}

/** What the last check found when there is nothing to install, or null. */
function checkResult(result: Status['update']['checkResult']): ReactNode {
  if (result === 'up-to-date')
    return <FormSuccess>No update available. You&apos;re on the latest version.</FormSuccess>;
  if (result === 'test-build') {
    return (
      <p className="text-subhead text-label-2">
        This is a test build, so it isn&apos;t compared with releases. Install a release to get update news.
      </p>
    );
  }
  return null;
}

/** Where RasBeacon announces a new version: an ntfy topic (a push to your phone) or a webhook. */
function NotificationsSection() {
  const { updateSettings, reload } = useDevice();

  return (
    <Loaded data={updateSettings} what="the notification settings" onRetry={() => reload('updateSettings')}>
      {(settings) => <NotificationsForm settings={settings} />}
    </Loaded>
  );
}

function NotificationsForm({ settings }: { settings: UpdateSettings }) {
  const { saveUpdateSettings, testUpdateNotification } = useDevice();
  const form = useForm({ notifyUrl: settings.notifyUrl }, (values) =>
    saveUpdateSettings({ checkHours: settings.checkHours, notifyUrl: values.notifyUrl.trim() }),
  );
  const testing = useAction(() => testUpdateNotification(form.values.notifyUrl.trim()));
  const [sent, setSent] = useState(false);

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        form.submit();
      }}
      noValidate
    >
      <Section
        title="Update notifications"
        footer={
          settings.checkHours === 0
            ? 'RasBeacon posts here once for each new version it finds. Choose how often to check above, so it looks by itself.'
            : 'RasBeacon posts here once for each new version it finds: an ntfy topic reaches your phone through the ntfy app, or use a Discord or Home Assistant webhook.'
        }
      >
        <TextField
          label="Address"
          labelNote="(optional)"
          inputMode="url"
          placeholder="https://ntfy.sh/your-topic"
          autoComplete="off"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          value={form.values.notifyUrl}
          onChange={(notifyUrl) => {
            form.setField('notifyUrl', notifyUrl);
            setSent(false);
          }}
          error={form.fieldErrors.notifyUrl ?? testing.fieldErrors.notifyUrl}
        />
        <div className="row flex-wrap gap-2">
          <Button type="submit" size="sm" loading={form.pending} disabled={!form.dirty}>
            Save
          </Button>
          <Button
            variant="secondary"
            size="sm"
            loading={testing.pending}
            disabled={form.values.notifyUrl.trim() === ''}
            onClick={async () => {
              setSent(false);
              setSent(await testing.run());
            }}
          >
            Send a test
          </Button>
        </div>
        {(form.error || testing.error || form.saved || sent) && (
          <div className="row">
            {form.error || testing.error ? (
              <FormError>{form.error ?? testing.error}</FormError>
            ) : (
              <FormSuccess>{sent ? 'Test sent: check that it arrived.' : 'Saved.'}</FormSuccess>
            )}
          </div>
        )}
      </Section>
    </form>
  );
}

/**
 * An installed update that RasBeacon hasn't restarted into yet (the restart failed, or the power
 * went off first): why, and the restart that finishes it, asked first like Reboot.
 */
function RestartPendingNotice({ message }: { message?: string }) {
  const { restartIntoUpdate } = useDevice();
  const [confirming, setConfirming] = useState(false);

  return (
    <Notice
      tone="warning"
      icon="system_update_alt"
      role="status"
      title="Restart to finish the update"
      actions={
        <Button variant="secondary" size="sm" icon="restart_alt" onClick={() => setConfirming(true)}>
          Restart
        </Button>
      }
    >
      {message && <p>{message}</p>}
      {confirming && (
        <AlertDialog
          title="Restart RasBeacon?"
          confirmLabel="Restart"
          busyLabel="Restarting…"
          onConfirm={restartIntoUpdate}
          onClose={() => setConfirming(false)}
        >
          <p>It starts the new version. Connected devices disconnect for about two minutes.</p>
        </AlertDialog>
      )}
    </Notice>
  );
}

/** A link to a release's page on GitHub, where its files are. */
function ReleaseLink({ url }: { url: string }) {
  return (
    <a href={url} target="_blank" rel="noreferrer" className={buttonClasses('secondary', 'sm')}>
      <Icon name="open_in_new" size={16} />
      <span>Release page</span>
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
      title={`RasBeacon ${reflash.version} needs a fresh install`}
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
        It can&apos;t be installed over {running} (it needs {reflash.minFromVersion} or newer). To move to it and keep
        your settings:
      </p>
      <ol className="flex list-decimal flex-col gap-0.5 pl-5">
        <li>
          Back up your settings
          {hasPlugins && <> (under Plugins, choose Plugin data or Everything to keep their data too)</>}, and keep the
          file and its password.
        </li>
        <li>Flash the {reflash.version} image for your board onto the SD card, with Raspberry Pi Imager.</li>
        <li>Set RasBeacon up, then restore the backup on this page, under Backup.</li>
      </ol>
      {backingUp && <BackupSheet hasPlugins={hasPlugins} onClose={() => setBackingUp(false)} />}
    </Notice>
  );
}

/**
 * The update buttons, in a row of their own: "Check now" asks GitHub (when this
 * build knows its releases), and turns into "Update now" once a version RasBeacon can download
 * and install is found; "Update from a file" installs a .rbupdate file from the Releases page
 * (for a RasBeacon without internet, or a version of your choice). Installing is confirmed
 * first. Under them: what the last check found, or how an install is going.
 */
function UpdateActions({ status }: { status: Status }) {
  const { installUpdate, downloadUpdate, checkForUpdates } = useDevice();
  const { update } = status;
  const online = status.uplink.connected;
  const input = useRef<HTMLInputElement>(null);
  // The file chosen, until its install is confirmed or cancelled.
  const [file, setFile] = useState<File | null>(null);
  const [confirmingDownload, setConfirmingDownload] = useState(false);
  const [installed, setInstalled] = useState(false);
  const installing = useAction(async (chosen: File) => {
    await installUpdate(chosen);
    setInstalled(true);
  });
  const busy = installing.pending || update.installing;
  const offered = update.canInstall && update.downloadSize ? update.latestVersion : undefined;
  const size = formatBytes(update.downloadSize ?? 0);

  // One line under the buttons: an install's progress or result, else the last check's.
  let below: ReactNode = null;
  if (installing.pending) {
    below = (
      <ActivityIndicator
        label="Uploading and checking the update… keep this page open (a few minutes)."
        className="text-subhead text-label"
      />
    );
  } else if (update.installing) {
    below = (
      <ActivityIndicator label={update.message ?? 'Installing the update…'} className="text-subhead text-label" />
    );
  } else if (installing.error) {
    below = <FormError>{installing.error}</FormError>;
  } else if (installed) {
    below = (
      <FormSuccess>Update installed. RasBeacon is restarting to try it; reconnect in about two minutes.</FormSuccess>
    );
  } else if (update.canCheckOnline && update.checkError) {
    below = <FormError>{update.checkError}</FormError>;
  } else if (update.canCheckOnline && !offered) {
    below = checkResult(update.checkResult);
  }

  return (
    <>
      {/* A row of their own, under a line like the rows above. On phones and tablets each
          button takes the whole width, one above the other; on computers they sit side by side,
          each as wide as its icon and label. */}
      <div className="row">
        <div className="flex w-full flex-col gap-2 lg:flex-row lg:items-center">
          {offered ? (
            <Button icon="system_update_alt" disabled={busy} onClick={() => setConfirmingDownload(true)}>
              Update now
            </Button>
          ) : (
            update.canCheckOnline && (
              <ActionButton icon="refresh" action={checkForUpdates} busyLabel="Checking…" disabled={!online || busy}>
                Check now
              </ActionButton>
            )
          )}
          {update.canInstall && (
            <Button
              variant="secondary"
              icon="upload"
              loading={installing.pending}
              disabled={busy}
              onClick={() => input.current?.click()}
            >
              {installing.pending ? 'Installing…' : 'Update from a file'}
            </Button>
          )}
        </div>
      </div>
      {update.canCheckOnline && !offered && !online && (
        <div className="row">
          <p className="text-subhead text-label-2">{NEEDS_INTERNET}</p>
        </div>
      )}
      {below && <div className="row">{below}</div>}

      {/* After the rows, so it doesn't break the line between them (`.row + .row`). */}
      {update.canInstall && (
        <input
          ref={input}
          type="file"
          accept=".rbupdate"
          hidden
          aria-label="Update file (.rbupdate)"
          onChange={(event) => {
            setFile(event.target.files?.[0] ?? null);
            setInstalled(false);
            event.target.value = '';
          }}
        />
      )}
      {confirmingDownload && offered && (
        <AlertDialog
          title={`Install RasBeacon ${offered}?`}
          confirmLabel="Install"
          busyLabel="Starting…"
          onConfirm={downloadUpdate}
          onClose={() => setConfirmingDownload(false)}
        >
          <p>
            RasBeacon downloads it ({size}) from GitHub and installs it, then restarts; connected devices are offline
            for about two minutes. If the new version doesn&apos;t start correctly, RasBeacon goes back to this one by
            itself.
          </p>
          {status.hotspot.metered && <p>This connection is metered: the download uses {size} of data.</p>}
        </AlertDialog>
      )}
      {file && (
        <AlertDialog
          title="Install the update?"
          confirmLabel="Install"
          busyLabel="Starting…"
          onConfirm={async () => {
            // The line under the buttons shows the progress; the alert closes at once.
            void installing.run(file);
          }}
          onClose={() => setFile(null)}
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
