import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Plugin, PluginEncryption } from '@/data/types';
import { saveTextFile } from '@/lib/download';
import { ActionButton, ButtonRow, Meter, Notice, Row, Section, Sheet, SwitchRow, TextField } from '@/ui';

/** "120 of 3,010 files", or "Counting the files…" before the count is known. */
function progressText(encryption: PluginEncryption): string {
  if (!encryption.total) return 'Counting the files…';
  return `${(encryption.done ?? 0).toLocaleString('en-US')} of ${encryption.total.toLocaleString('en-US')} files`;
}

/**
 * Encrypting the plugin's USB drive, so someone who pulls it out can't read its files on a
 * computer; only the plugin shows them. Turning it on or off pauses the plugin while the files
 * move. Once they are encrypted, the recovery key can be downloaded, and the page asks for it to
 * be saved until it is; turning encryption off needs it. Shown for a plugin with a chosen drive.
 */
export function EncryptionSection({ plugin }: { plugin: Plugin }) {
  const { setPluginEncryption, pluginRecoveryKey } = useDevice();
  const [asking, setAsking] = useState<'on' | 'off' | null>(null);
  const encryption = plugin.encryption;
  if (!encryption) return null;

  const moving = encryption.state === 'encrypting' || encryption.state === 'decrypting';
  // A move that stopped (the drive was full or pulled out): it goes on from where it stopped.
  const stopped = moving && plugin.state !== 'converting';
  // The switch shows where the drive is going, also while the files move.
  const on = encryption.state === 'on' || encryption.state === 'encrypting';
  const percent = encryption.total ? ((encryption.done ?? 0) / encryption.total) * 100 : 0;

  async function downloadKey() {
    const file = await pluginRecoveryKey(plugin.id);
    saveTextFile(file.fileName, file.text);
  }

  return (
    <>
      {encryption.state === 'on' && !encryption.recoveryKeySaved && (
        <Notice
          tone="warning"
          icon="key"
          title="Save your recovery key"
          actions={
            <ActionButton size="sm" action={downloadKey}>
              Download recovery key
            </ActionButton>
          }
        >
          <p>
            You need it to turn encryption off, and to read the drive’s files on a computer if RasBeacon’s SD card is
            lost. Keep it somewhere safe, away from the drive.
          </p>
        </Notice>
      )}

      <Section
        title="Encryption"
        footer={`Someone who pulls out the drive can’t read its files on a computer: only ${plugin.name} shows them. Turning this on or off pauses ${plugin.name} until every file is moved, which takes a while on a full drive.`}
      >
        <SwitchRow
          leading={{ tile: 'lock', color: 'indigo' }}
          title="Encrypt the drive’s files"
          checked={on}
          // Asks first: the sheets say what happens (and turning it off needs the key).
          onChange={async (checked) => setAsking(checked ? 'on' : 'off')}
        />
        {moving && (
          <div className="row flex-col items-stretch gap-2 py-3">
            <Row
              className="p-0"
              title={encryption.state === 'encrypting' ? 'Encrypting' : 'Decrypting'}
              value={progressText(encryption)}
            />
            <Meter percent={percent} label={encryption.state === 'encrypting' ? 'Encrypting' : 'Decrypting'} />
          </div>
        )}
        {stopped && (
          <ButtonRow
            onClick={() =>
              encryption.state === 'encrypting' ? void setPluginEncryption(plugin.id, true) : setAsking('off')
            }
          >
            {encryption.state === 'encrypting' ? 'Go on encrypting' : 'Go on decrypting'}
          </ButtonRow>
        )}
        {encryption.state === 'on' && <ButtonRow onClick={() => void downloadKey()}>Download recovery key</ButtonRow>}
      </Section>

      {asking === 'on' && (
        <Sheet
          title="Encrypt the drive?"
          confirmLabel="Encrypt"
          busyLabel="Starting…"
          onConfirm={() => setPluginEncryption(plugin.id, true)}
          onClose={() => setAsking(null)}
        >
          <Notice tone="info" icon="lock">
            <p>
              {plugin.name} pauses while RasBeacon moves each file into an encrypted folder on the drive, and starts
              again when they are all moved. Keep the drive plugged in; if it is pulled out, RasBeacon goes on where it
              stopped.
            </p>
            <p>
              Afterwards, download the recovery key: it is the only way to read the files without RasBeacon, and to turn
              encryption off.
            </p>
          </Notice>
        </Sheet>
      )}
      {asking === 'off' && <DecryptSheet plugin={plugin} onClose={() => setAsking(null)} />}
    </>
  );
}

/** Turning encryption off: the drive's recovery key, then the files move back out. */
function DecryptSheet({ plugin, onClose }: { plugin: Plugin; onClose: () => void }) {
  const { setPluginEncryption } = useDevice();
  const [key, setKey] = useState('');

  return (
    <Sheet
      title="Decrypt the drive?"
      confirmLabel="Decrypt"
      busyLabel="Starting…"
      confirmDisabled={key.trim() === ''}
      onConfirm={() => setPluginEncryption(plugin.id, false, key)}
      onClose={onClose}
    >
      {(errors) => (
        <Section
          level={3}
          footer={`${plugin.name} pauses while RasBeacon moves the files back out of the encrypted folder. Afterwards anyone with the drive can read them.`}
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
