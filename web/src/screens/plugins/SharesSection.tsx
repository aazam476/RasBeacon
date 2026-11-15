import { useState } from 'react';
import { useDevice } from '@/data/device';
import { FieldErrors } from '@/data/errors';
import type { Plugin, PluginShare, PluginShares } from '@/data/types';
import { saveTextFile } from '@/lib/download';
import {
  ActionButton,
  AlertDialog,
  BreakableText,
  ButtonRow,
  FormSwitchRow,
  Meter,
  Notice,
  Row,
  Section,
  SelectField,
  Sheet,
  SheetRow,
  TextField,
} from '@/ui';
import { SettingField } from './PluginFields';
import { useDriveList } from './useDriveList';
import { sharePlace, shareUsage, storageGbText } from './pluginDisplay';

/** Megabytes in a gigabyte, as RasBeacon counts a share's quota. */
const MB_PER_GB = 1024;

/** Typing names and folders: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCorrect: 'off', spellCheck: false } as const;

/** A quota typed in GB as MB, or a message for the field when it isn't a size. */
function quotaOf(typed: string): number {
  const text = typed.trim().replace(',', '.');
  const gb = Number(text);
  if (text === '') throw new FieldErrors({ quotaMb: 'Enter a quota in GB.' });
  if (!Number.isFinite(gb) || gb <= 0) throw new FieldErrors({ quotaMb: 'Enter a quota in GB, like 2 or 1.5.' });
  return Math.round(gb * MB_PER_GB);
}

/** A share's settings as typed, from the settings it has (or a new share's defaults). */
function settingValues(share: { settings: PluginShare['settings'] }): Record<string, string> {
  return Object.fromEntries(share.settings.map((setting) => [setting.key, setting.value]));
}

/**
 * A plugin's shares (Samba's): each with a name, a place that never changes and a quota. On
 * RasBeacon's internal storage the quota's space is set aside on the SD card; on a USB drive a
 * share is a folder, and shows as full once its files reach the quota. Each share can have its
 * files encrypted. Shown for a plugin whose manifest has shares; it starts with none.
 */
export function SharesSection({ plugin }: { plugin: Plugin }) {
  const { pluginRecoveryKey } = useDevice();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string>();
  // The share an action is about, opened from its sheet (one dialog at a time, never over a sheet).
  const [action, setAction] = useState<{ kind: 'encrypt' | 'decrypt' | 'remove'; share: string }>();
  const shares = plugin.shares;
  if (!shares) return null;

  const edited = shares.list.find((share) => share.id === editing);
  const acted = shares.list.find((share) => share.id === action?.share);
  const full = shares.list.length >= shares.max;
  const unsaved = shares.list.filter((share) => share.encryption.state === 'on' && !share.encryption.recoveryKeySaved);

  async function downloadKey(share: PluginShare) {
    const file = await pluginRecoveryKey(plugin.id, share.id);
    saveTextFile(file.fileName, file.text);
  }

  return (
    <>
      {unsaved.map((share) => (
        <Notice
          key={share.id}
          tone="warning"
          icon="key"
          title={`Save the recovery key of ${share.name}`}
          actions={
            <ActionButton size="sm" action={() => downloadKey(share)}>
              Download recovery key
            </ActionButton>
          }
        >
          <p>
            You need it to turn the share’s encryption off, and to read its files on a computer if RasBeacon’s SD card
            is lost. Keep it somewhere safe, away from the drive.
          </p>
        </Notice>
      ))}

      <Section
        title="Shares"
        footer="Where a share is never changes. On RasBeacon’s internal storage, its quota is set aside on the SD card. On a USB drive, a share is a folder, and computers see it as full once its files reach the quota."
      >
        {shares.list.length === 0 && <Row title="No shares yet" subtitle={`Add one for ${plugin.name} to offer.`} />}
        {shares.list.map((share) => (
          <SheetRow
            key={share.id}
            leading={{
              tile: share.encryption.state === 'on' ? 'lock' : share.drive ? 'usb' : 'folder',
              color: share.encryption.state === 'on' ? 'indigo' : 'blue',
            }}
            title={share.name}
            subtitle={<ShareSubtitle share={share} />}
            value={<span className={share.usedMb > share.quotaMb ? 'text-red' : undefined}>{shareUsage(share)}</span>}
            onClick={() => setEditing(share.id)}
          />
        ))}
        <ButtonRow
          onClick={() => setAdding(true)}
          disabled={full}
          note={full ? `${plugin.name} can have at most ${shares.max} shares.` : undefined}
        >
          Add a share
        </ButtonRow>
      </Section>

      {adding && <AddShareSheet plugin={plugin} shares={shares} onClose={() => setAdding(false)} />}
      {edited && (
        <ShareSheet
          plugin={plugin}
          share={edited}
          onClose={() => setEditing(undefined)}
          onAction={(kind) => {
            setEditing(undefined);
            setAction({ kind, share: edited.id });
          }}
          onDownloadKey={() => downloadKey(edited)}
        />
      )}
      {acted && action?.kind === 'encrypt' && (
        <EncryptDialog plugin={plugin} share={acted} onClose={() => setAction(undefined)} />
      )}
      {acted && action?.kind === 'decrypt' && (
        <DecryptSheet plugin={plugin} share={acted} onClose={() => setAction(undefined)} />
      )}
      {acted && action?.kind === 'remove' && (
        <RemoveShareDialog plugin={plugin} share={acted} onClose={() => setAction(undefined)} />
      )}
    </>
  );
}

/** Under a share's name: where it is, and what is happening to it. */
function ShareSubtitle({ share }: { share: PluginShare }) {
  const { encryption } = share;
  if (encryption.state === 'encrypting' || encryption.state === 'decrypting') {
    const action = encryption.state === 'encrypting' ? 'Encrypting' : 'Decrypting';
    return (
      <>
        {encryption.total
          ? `${action}: ${(encryption.done ?? 0).toLocaleString('en-US')} of ${encryption.total.toLocaleString('en-US')} files`
          : `${action}…`}
        <Meter
          className="mt-1.5"
          percent={encryption.total ? ((encryption.done ?? 0) / encryption.total) * 100 : 0}
          label={`${action} ${share.name}`}
        />
      </>
    );
  }
  // A long folder path wraps at its slashes instead of being cut off.
  const place = <BreakableText>{sharePlace(share)}</BreakableText>;
  if (share.driveMissing) return <span className="text-orange">{place} · drive not plugged in</span>;
  if (share.usedMb > share.quotaMb) return <span className="text-red">{place} · over its quota</span>;
  return place;
}

/** A new share: its name, where it goes (fixed from then on), its quota and settings. */
function AddShareSheet({ plugin, shares, onClose }: { plugin: Plugin; shares: PluginShares; onClose: () => void }) {
  const { addPluginShare } = useDevice();
  const [name, setName] = useState('');
  const [drive, setDrive] = useState('');
  // The folder follows the name until it is typed in.
  const [folder, setFolder] = useState<string>();
  const [quota, setQuota] = useState('');
  const [settings, setSettings] = useState(() => settingValues(shares));
  const { drives, problem: driveProblem } = useDriveList(shares.onDrives);

  const places = [
    { value: '', label: 'RasBeacon internal storage' },
    ...(drives ?? [])
      .filter((d) => !d.usedBy || d.usedBy === plugin.name)
      .map((d) => ({ value: d.uuid, label: `${d.label || 'Untitled'} (USB drive)` })),
  ];
  const quotaHint = drive
    ? 'Up to what the drive has free, less what its other shares may still fill.'
    : `Up to ${storageGbText(shares.newInternalMaxMb, 'down')} GB: RasBeacon keeps 2 GB of its SD card free.`;

  return (
    <Sheet
      title="Add a share"
      confirmLabel="Add"
      busyLabel="Adding…"
      confirmDisabled={name.trim() === ''}
      onConfirm={() =>
        addPluginShare(plugin.id, {
          name: name.trim(),
          drive,
          folder: drive ? (folder ?? name).trim() : '',
          quotaMb: quotaOf(quota),
          settings,
        })
      }
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section level={3}>
            <TextField
              label="Name"
              hint="What computers see the share as."
              value={name}
              onChange={setName}
              error={errors.name}
              maxLength={40}
              {...PLAIN_INPUT}
            />
          </Section>
          <Section level={3} title="Where" footer="It stays where you put it: a share can’t move later.">
            {shares.onDrives && (
              <SelectField
                label="Storage"
                options={places}
                value={drive}
                onChange={setDrive}
                disabled={drives === null && !driveProblem}
                hint={drives === null && !driveProblem ? 'Looking for USB drives…' : (driveProblem ?? undefined)}
                error={errors.drive}
              />
            )}
            {drive && (
              <TextField
                label="Folder on the drive"
                hint="Files already in it become the share’s. Like Movies or Media/Movies."
                value={folder ?? name}
                onChange={setFolder}
                error={errors.folder}
                {...PLAIN_INPUT}
              />
            )}
            <TextField
              label="Quota"
              labelNote="(GB)"
              hint={quotaHint}
              inputMode="decimal"
              value={quota}
              onChange={(typed) => setQuota(typed.replace(/[^\d.,]/g, ''))}
              error={errors.quotaMb}
              {...PLAIN_INPUT}
            />
          </Section>
          {shares.settings.length > 0 && (
            <Section level={3} title="Settings">
              {shares.settings.map((setting) => (
                <SettingField
                  key={setting.key}
                  setting={setting}
                  value={settings[setting.key] ?? ''}
                  onChange={(value) => setSettings((current) => ({ ...current, [setting.key]: value }))}
                  error={errors[`settings.${setting.key}`]}
                />
              ))}
            </Section>
          )}
        </>
      )}
    </Sheet>
  );
}

interface ShareSheetProps {
  plugin: Plugin;
  share: PluginShare;
  onClose: () => void;
  /** Encrypting, decrypting or removing it: the sheet closes and the page asks. */
  onAction: (kind: 'encrypt' | 'decrypt' | 'remove') => void;
  onDownloadKey: () => Promise<void>;
}

/** One share: its name, quota and settings to change, its encryption, and removing it. */
function ShareSheet({ plugin, share, onClose, onAction, onDownloadKey }: ShareSheetProps) {
  const { savePluginShare } = useDevice();
  const [name, setName] = useState(share.name);
  const [quota, setQuota] = useState(storageGbText(share.quotaMb));
  const [settings, setSettings] = useState(() => settingValues(share));
  const moving = share.encryption.state === 'encrypting' || share.encryption.state === 'decrypting';
  const used = share.usedMb >= 0 ? ` Its files use ${storageGbText(share.usedMb)} GB.` : '';

  return (
    <Sheet
      title={share.name}
      confirmLabel="Save"
      busyLabel="Saving…"
      confirmDisabled={name.trim() === '' || moving}
      onConfirm={() => savePluginShare(plugin.id, share.id, { name: name.trim(), quotaMb: quotaOf(quota), settings })}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section level={3} footer={`Saving restarts ${plugin.name}, which stops a copy in progress.`}>
            <Row title="Where" value={sharePlace(share)} />
            <TextField
              label="Name"
              value={name}
              onChange={setName}
              error={errors.name}
              maxLength={40}
              {...PLAIN_INPUT}
            />
            <TextField
              label="Quota"
              labelNote="(GB)"
              hint={`From ${storageGbText(share.minQuotaMb, 'up')} to ${storageGbText(share.maxQuotaMb, 'down')} GB.${used}`}
              inputMode="decimal"
              value={quota}
              onChange={(typed) => setQuota(typed.replace(/[^\d.,]/g, ''))}
              error={errors.quotaMb}
              {...PLAIN_INPUT}
            />
            {share.settings.map((setting) => (
              <SettingField
                key={setting.key}
                setting={setting}
                value={settings[setting.key] ?? ''}
                onChange={(value) => setSettings((current) => ({ ...current, [setting.key]: value }))}
                error={errors[`settings.${setting.key}`]}
              />
            ))}
          </Section>

          <Section
            level={3}
            title="Encryption"
            footer={`Someone who pulls out the ${share.drive ? 'drive' : 'SD card'} can’t read encrypted files on a computer: only ${plugin.name} shows them. Turning it on or off pauses ${plugin.name} until every file is moved.`}
          >
            <FormSwitchRow
              leading={{ tile: 'lock', color: 'indigo' }}
              label="Encrypt this share’s files"
              checked={share.encryption.state === 'on' || share.encryption.state === 'encrypting'}
              disabled={moving}
              onChange={(on) => onAction(on ? 'encrypt' : 'decrypt')}
            />
            {share.encryption.state === 'on' && (
              <ButtonRow onClick={() => void onDownloadKey()}>Download recovery key</ButtonRow>
            )}
          </Section>

          <Section level={3}>
            <ButtonRow tone="danger" onClick={() => onAction('remove')} disabled={moving}>
              Remove share
            </ButtonRow>
          </Section>
        </>
      )}
    </Sheet>
  );
}

/** Before encrypting a share: what happens, and the recovery key to save afterwards. */
function EncryptDialog({ plugin, share, onClose }: { plugin: Plugin; share: PluginShare; onClose: () => void }) {
  const { setPluginEncryption } = useDevice();
  return (
    <AlertDialog
      title={`Encrypt ${share.name}?`}
      confirmLabel="Encrypt"
      busyLabel="Starting…"
      onConfirm={() => setPluginEncryption(plugin.id, share.id, true)}
      onClose={onClose}
    >
      {plugin.name} pauses while RasBeacon moves each file into an encrypted folder, and starts again when they are all
      moved. Afterwards, download the recovery key: it is the only way to read the files without RasBeacon, and to turn
      encryption off.
    </AlertDialog>
  );
}

/** Turning a share's encryption off: its recovery key, then the files move back out. */
function DecryptSheet({ plugin, share, onClose }: { plugin: Plugin; share: PluginShare; onClose: () => void }) {
  const { setPluginEncryption } = useDevice();
  const [key, setKey] = useState('');
  return (
    <Sheet
      title={`Decrypt ${share.name}?`}
      confirmLabel="Decrypt"
      busyLabel="Starting…"
      confirmDisabled={key.trim() === ''}
      onConfirm={() => setPluginEncryption(plugin.id, share.id, false, key)}
      onClose={onClose}
    >
      {(errors) => (
        <Section
          level={3}
          footer={`${plugin.name} pauses while RasBeacon moves the files back out of the encrypted folder. Afterwards anyone with the ${share.drive ? 'drive' : 'SD card'} can read them.`}
        >
          <TextField
            label="Recovery key"
            hint="From the file you downloaded, like ABCDE-FGHIJ-…"
            value={key}
            onChange={setKey}
            error={errors.recoveryKey}
            autoComplete="off"
            autoCapitalize="characters"
            autoCorrect="off"
            spellCheck={false}
          />
        </Section>
      )}
    </Sheet>
  );
}

/**
 * Removing a share. On internal storage, or encrypted, its files go with it; on a drive its
 * folder stays, unless the owner asks for it to be deleted too.
 */
function RemoveShareDialog({ plugin, share, onClose }: { plugin: Plugin; share: PluginShare; onClose: () => void }) {
  const { removePluginShare } = useDevice();
  const encrypted = share.encryption.state !== 'off';
  // A share of a whole drive (carried over from an older version) never deletes the drive's files.
  const wholeDrive = Boolean(share.drive) && !share.folder;
  const canKeep = Boolean(share.drive) && !encrypted && !wholeDrive;
  const [deleteFiles, setDeleteFiles] = useState(false);
  const deleting = !wholeDrive && (!canKeep || deleteFiles);

  return (
    <AlertDialog
      title={`Remove ${share.name}?`}
      confirmLabel={deleting ? 'Remove and delete' : 'Remove'}
      busyLabel="Removing…"
      tone={deleting ? 'danger' : 'default'}
      onConfirm={() => removePluginShare(plugin.id, share.id, deleteFiles)}
      onClose={onClose}
    >
      {!share.drive && <>Its files are deleted with it, and its space goes back to the SD card.</>}
      {wholeDrive && (
        <>
          {plugin.name} stops sharing it. The drive’s files stay on it
          {encrypted ? ', encrypted: you need its recovery key to read them.' : '.'}
        </>
      )}
      {share.drive && encrypted && !wholeDrive && (
        <>Its folder {share.folder} is deleted from the drive with it: its encrypted files can’t be read without it.</>
      )}
      {canKeep && (
        <>
          {plugin.name} stops sharing it. Its folder {share.folder} stays on the drive with its files, unless you delete
          it too.
          <FormSwitchRow
            className="mt-3 rounded-lg bg-fill px-3"
            label="Also delete the folder and its files"
            checked={deleteFiles}
            onChange={setDeleteFiles}
          />
        </>
      )}
    </AlertDialog>
  );
}
