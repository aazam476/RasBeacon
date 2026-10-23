import { useState } from 'react';
import { useDevice, type BackupPlugins } from '@/data/device';
import { AlertDialog, ButtonRow, Choice, ChoiceList, FileField, PasswordField, Section, Sheet } from '@/ui';

/**
 * Backups: all settings and the plugins (with their data and apps, if wanted) in one file
 * encrypted with a password, to restore on this RasBeacon or move to another one. Being logged
 * in is enough to make or restore one.
 */
export function BackupSection() {
  const { status } = useDevice();
  const [open, setOpen] = useState<'backup' | 'restore' | null>(null);
  const [restored, setRestored] = useState(false);
  const plugins = status.data?.plugins.length ?? 0;

  return (
    <Section
      title="Backup"
      footer="A backup holds every setting, password and key, encrypted with a password you choose. It can move your settings to another RasBeacon."
    >
      <ButtonRow leading={{ tile: 'download', color: 'blue' }} onClick={() => setOpen('backup')}>
        Back up settings
      </ButtonRow>
      <ButtonRow leading={{ tile: 'settings_backup_restore', color: 'orange' }} onClick={() => setOpen('restore')}>
        Restore from a backup
      </ButtonRow>
      {open === 'backup' && <BackupSheet hasPlugins={plugins > 0} onClose={() => setOpen(null)} />}
      {open === 'restore' && (
        <RestoreSheet
          onClose={() => setOpen(null)}
          onRestored={() => {
            setOpen(null);
            setRestored(true);
          }}
        />
      )}
      {restored && (
        <AlertDialog
          title="Restored"
          confirmLabel="OK"
          busyLabel="OK"
          onConfirm={async () => undefined}
          onClose={() => setRestored(false)}
        >
          <p>
            RasBeacon is restarting with the backup&apos;s settings. Join its Wi-Fi network again in a minute, and log
            in with the backup&apos;s admin password.
          </p>
        </AlertDialog>
      )}
    </Section>
  );
}

/** The sheet that makes a backup file. The software section opens it too, before a fresh install. */
export function BackupSheet({ hasPlugins, onClose }: { hasPlugins: boolean; onClose: () => void }) {
  const { backUp } = useDevice();
  const [password, setPassword] = useState('');
  const [plugins, setPlugins] = useState<BackupPlugins>('settings');

  return (
    <Sheet
      title="Back up settings"
      confirmLabel="Back up"
      busyLabel="Checking…"
      onConfirm={() => backUp(password, plugins)}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section
            level={3}
            footer="At least 8 characters. Nobody can restore the backup without it, so keep it somewhere safe."
          >
            <PasswordField
              label="Password for the backup"
              autoComplete="new-password"
              initiallyVisible
              value={password}
              onChange={setPassword}
              error={errors.backupPassword}
            />
          </Section>
          {hasPlugins && (
            <Section
              level={3}
              title="Plugins"
              footer="Apps installed from a file are always in the backup, since they can't be downloaded again. Running plugins pause for a moment while their data is copied."
            >
              <ChoiceList ariaLabel="Plugins" value={plugins} onChange={setPlugins}>
                <Choice
                  value="settings"
                  title="Settings only"
                  description="Plugins come back with their settings. Their apps are downloaded again, and their data starts empty."
                />
                <Choice
                  value="data"
                  title="Plugin data"
                  description="Also their data. Their apps are downloaded again."
                />
                <Choice
                  value="everything"
                  title="Everything"
                  description="Also their apps, so restoring works offline. The largest backup."
                />
              </ChoiceList>
            </Section>
          )}
        </>
      )}
    </Sheet>
  );
}

function RestoreSheet({ onClose, onRestored }: { onClose: () => void; onRestored: () => void }) {
  const { restore } = useDevice();
  const [file, setFile] = useState<File | null>(null);
  const [password, setPassword] = useState('');

  return (
    <Sheet
      title="Restore from a backup"
      confirmLabel="Restore"
      busyLabel="Restoring…"
      tone="danger"
      onConfirm={async () => {
        if (!file) throw new Error('Choose the backup file.');
        await restore(file, password);
        onRestored();
      }}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section
            level={3}
            footer="Every setting is replaced by the backup's and RasBeacon restarts. Installed plugins are removed and the backup's plugins are installed with their settings and data."
          >
            <FileField
              label="Backup file"
              labelNote="(.rbbackup)"
              accept=".rbbackup"
              onChange={(files) => setFile(files[0] ?? null)}
              error={errors.file}
            />
            <PasswordField
              label="The backup's password"
              autoComplete="off"
              value={password}
              onChange={setPassword}
              error={errors.backupPassword}
            />
          </Section>
        </>
      )}
    </Sheet>
  );
}
