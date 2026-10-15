import { useState } from 'react';
import { useDevice } from '@/data/device';
import { AlertDialog, ButtonRow, FileField, FormSwitchRow, PasswordField, Section, Sheet } from '@/ui';

/**
 * Backups: all settings (and, if wanted, the plugins with their data) in one file encrypted
 * with a password, to restore on this RasBeacon or move to another one.
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

function BackupSheet({ hasPlugins, onClose }: { hasPlugins: boolean; onClose: () => void }) {
  const { backUp } = useDevice();
  const [adminPassword, setAdminPassword] = useState('');
  const [password, setPassword] = useState('');
  const [includePlugins, setIncludePlugins] = useState(false);

  return (
    <Sheet
      title="Back up settings"
      confirmLabel="Back up"
      busyLabel="Making the backup…"
      onConfirm={() => backUp(adminPassword, password, includePlugins)}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section level={3} footer="The backup holds your passwords, so RasBeacon asks for its admin password again.">
            <PasswordField
              label="Admin password"
              autoComplete="current-password"
              value={adminPassword}
              onChange={setAdminPassword}
              error={errors.adminPassword}
            />
          </Section>
          <Section
            level={3}
            footer="At least 8 characters. Without it the backup can't be opened, by anyone: keep it somewhere safe."
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
              footer="Their images and data make the backup much larger. Running plugins pause for a moment while their data is copied."
            >
              <FormSwitchRow label="Include plugins" checked={includePlugins} onChange={setIncludePlugins} />
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
  const [adminPassword, setAdminPassword] = useState('');
  const [password, setPassword] = useState('');

  return (
    <Sheet
      title="Restore from a backup"
      confirmLabel="Restore"
      busyLabel="Restoring…"
      tone="danger"
      onConfirm={async () => {
        if (!file) throw new Error('Choose the backup file.');
        await restore(file, adminPassword, password);
        onRestored();
      }}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section
            level={3}
            footer="Every setting is replaced by the backup's, and RasBeacon restarts. A backup with plugins replaces the installed plugins; one without keeps them."
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
          <Section level={3}>
            <PasswordField
              label="Admin password"
              labelNote="(this RasBeacon's)"
              autoComplete="current-password"
              value={adminPassword}
              onChange={setAdminPassword}
              error={errors.adminPassword}
            />
          </Section>
        </>
      )}
    </Sheet>
  );
}
