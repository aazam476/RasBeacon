import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import { useAction } from '@/lib/useAction';
import { FileField, FormActions, Section, TextAreaField } from '@/ui';

/**
 * Adds a WireGuard profile from a file or pasted text. The router checks the file and refuses
 * unsafe ones (for example lines that would run commands); its message is shown here.
 */
export function ImportProfileSection() {
  const { importVpnProfile } = useDevice();
  const [text, setText] = useState('');
  const [fileName, setFileName] = useState<string | undefined>();
  // A file input can't be emptied from code; a new key gives a fresh one after an import.
  const [fileInputKey, setFileInputKey] = useState(0);
  const [imported, setImported] = useState(false);
  const importing = useAction(() => importVpnProfile({ text, fileName }));

  async function chooseFile(files: File[]) {
    const file = files[0];
    setImported(false);
    importing.clearErrors();
    setFileName(file?.name);
    setText(file ? await file.text() : '');
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await importing.run()) {
      setText('');
      setFileName(undefined);
      setFileInputKey((key) => key + 1);
      setImported(true);
    }
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
      <Section
        title="Import a WireGuard file"
        footer="Your VPN provider or WireGuard server gives you a .conf file. RasBeacon checks it first."
      >
        <FileField key={fileInputKey} label="WireGuard file" accept=".conf,text/plain" onChange={chooseFile} />
        <TextAreaField
          label="Or paste its contents"
          mono
          rows={6}
          placeholder={'[Interface]\nPrivateKey = …\n\n[Peer]\n…'}
          value={text}
          onChange={(value) => {
            setText(value);
            setImported(false);
            importing.clearErrors();
          }}
        />
      </Section>
      <FormActions
        label="Import"
        busyLabel="Importing…"
        pending={importing.pending}
        disabled={text.trim() === ''}
        error={importing.error}
        success={imported && 'WireGuard file imported.'}
      />
    </form>
  );
}
