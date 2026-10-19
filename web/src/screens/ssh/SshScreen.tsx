import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { SSHSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { FileField, FormActions, FormSwitchRow, Row, Section, TextAreaField } from '@/ui';

/** Lines that hold a key (not blank, not a comment). */
function countKeys(text: string): number {
  return text.split('\n').filter((line) => line.trim() !== '' && !line.trim().startsWith('#')).length;
}

/** Command-line access for advanced users: off by default, public keys only. */
export function SshScreen() {
  const { sshSettings, reload } = useDevice();

  return (
    <Loaded data={sshSettings} what="the SSH settings" onRetry={() => reload('sshSettings')}>
      {(settings) => (
        <>
          <SshForm settings={settings} />
          <HowToConnect />
        </>
      )}
    </Loaded>
  );
}

function SshForm({ settings }: { settings: SSHSettings }) {
  const { saveSshSettings } = useDevice();
  const form = useForm(settings, saveSshSettings);
  const { values, setField } = form;
  // A file input can't be emptied from code; a new key gives a fresh one after each file.
  const [fileInputKey, setFileInputKey] = useState(0);
  const keys = countKeys(values.authorizedKeys);

  /** Adds the keys from the chosen .pub files to the end of the list. */
  async function addKeyFiles(files: File[]) {
    const texts = await Promise.all(files.map((file) => file.text()));
    form.update(
      (current) => ({
        ...current,
        authorizedKeys:
          [current.authorizedKeys.trimEnd(), ...texts.map((text) => text.trim())].filter(Boolean).join('\n') + '\n',
      }),
      ['authorizedKeys'],
    );
    setFileInputKey((key) => key + 1);
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
      <Section
        footer={
          <>
            SSH is off by default. When on, it only accepts the public keys below (no passwords) and only from devices
            on the hotspot. Log in as <code className="font-mono text-label">root</code>.
          </>
        }
      >
        <FormSwitchRow
          leading={{ tile: 'terminal', color: 'gray' }}
          label="Turn on SSH"
          description="Needs at least one public key below."
          checked={values.enabled}
          onChange={(enabled) => setField('enabled', enabled)}
        />
      </Section>
      {/* As on the VPN page: the file first, then the text it fills in. */}
      <Section title="Public keys" footer={`${keys} ${keys === 1 ? 'key' : 'keys'}.`}>
        <FileField
          key={fileInputKey}
          label="Add from a file"
          labelNote="(.pub)"
          accept=".pub,text/plain"
          multiple
          onChange={addKeyFiles}
        />
        <TextAreaField
          label="Or paste its contents"
          labelNote="(one key per line, such as ~/.ssh/id_ed25519.pub)"
          mono
          rows={5}
          placeholder="ssh-ed25519 AAAA… you@laptop"
          value={values.authorizedKeys}
          onChange={(authorizedKeys) => setField('authorizedKeys', authorizedKeys)}
        />
      </Section>
      <FormActions
        label="Save"
        busyLabel="Saving…"
        pending={form.pending}
        error={form.error}
        success={form.saved && 'Saved.'}
      />
    </form>
  );
}

/** The command to connect with, using the hotspot's address. */
function HowToConnect() {
  const { status } = useDevice();
  const address = status.data?.hotspot.lanAddress ?? 'the hotspot address';

  return (
    <Section
      title="How to connect"
      footer={
        <>
          From a device on the hotspot whose key is in the list. No key yet?{' '}
          <code className="font-mono text-label">ssh-keygen -t ed25519</code> makes one; paste the contents of the .pub
          file it writes.
        </>
      }
    >
      <Row
        leading={{ icon: 'terminal' }}
        title={
          // One tap selects the whole command, ready to copy.
          <span className="selectable font-mono text-callout break-all select-all">ssh root@{address}</span>
        }
      />
    </Section>
  );
}
