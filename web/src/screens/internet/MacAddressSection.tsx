import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { Client, MACChoice, UplinkSettings } from '@/data/types';
import { useAction } from '@/lib/useAction';
import { FormActions, Notice, Row, Section, SelectField, TextField } from '@/ui';
import { clientName } from '../common/clientDisplay';

type Mode = '' | 'random' | 'cloned';

const MODE_OPTIONS: readonly { value: Mode; label: string }[] = [
  { value: '', label: 'The Pi’s own' },
  { value: 'random', label: 'Random' },
  { value: 'cloned', label: 'Cloned from another device' },
];

/** One uplink's choice as it is being edited. */
interface Draft {
  mode: Mode;
  /** The cloned address as typed. */
  cloned: string;
}

function draftOf(choice: MACChoice | undefined): Draft {
  const mode = choice?.mode ?? '';
  return { mode, cloned: mode === 'cloned' ? (choice?.address ?? '') : '' };
}

/** What a draft saves: a random one keeps the address it has (the router keeps it). */
function choiceOf(draft: Draft, saved: MACChoice | undefined): MACChoice {
  if (draft.mode === 'random') return { mode: 'random', address: saved?.mode === 'random' ? saved.address : undefined };
  if (draft.mode === 'cloned') return { mode: 'cloned', address: draft.cloned.trim().toLowerCase() };
  return { mode: '' };
}

/** One uplink's address: the choice, the random one in use, or the cloned one with a shortcut. */
function UplinkMac({
  label,
  draft,
  onChange,
  saved,
  clients,
  error,
}: {
  label: string;
  draft: Draft;
  onChange: (draft: Draft) => void;
  saved: MACChoice | undefined;
  clients: Client[];
  error?: string;
}) {
  const typed = draft.cloned.trim().toLowerCase();
  // Two devices with one address on the hotspot side confuse the network.
  const sameDevice = draft.mode === 'cloned' ? clients.find((client) => client.mac === typed) : undefined;
  const deviceOptions = [
    { value: '', label: 'Choose…' },
    ...clients.map((client) => ({ value: client.mac, label: clientName(client) })),
  ];
  return (
    <>
      <SelectField
        label={label}
        options={MODE_OPTIONS}
        value={draft.mode}
        onChange={(mode) => onChange({ ...draft, mode })}
        error={draft.mode === 'cloned' ? undefined : error}
      />
      {draft.mode === 'random' && saved?.mode === 'random' && saved.address && (
        <Row title="Random address" value={<span className="font-mono">{saved.address}</span>} />
      )}
      {draft.mode === 'cloned' && (
        <>
          <TextField
            label="Address to use"
            placeholder="3a:1f:22:aa:bb:cc"
            mono
            value={draft.cloned}
            onChange={(cloned) => onChange({ ...draft, cloned })}
            error={error}
            hint={
              sameDevice
                ? `${clientName(sameDevice)} on your hotspot uses this address. Disconnect it from your hotspot first: two devices with one address confuse the network.`
                : undefined
            }
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
          />
          {clients.length > 0 && (
            <SelectField
              label="Copy a device’s address"
              options={deviceOptions}
              value=""
              onChange={(mac) => mac && onChange({ ...draft, cloned: mac })}
            />
          )}
        </>
      )}
    </>
  );
}

/**
 * The MAC addresses RasBeacon shows the networks it joins: the Pi's own, a random one, or a
 * cloned one, so a network that only lets in a device it knows (a hotel that registered your
 * laptop, a modem that remembers one device) lets RasBeacon in.
 */
export function MacAddressSection({ settings, clients }: { settings: UplinkSettings; clients: Client[] }) {
  const { saveUplinkMacs } = useDevice();
  const [wifi, setWifi] = useState(() => draftOf(settings.wifiMac));
  const [ethernet, setEthernet] = useState(() => draftOf(settings.ethernetMac));
  const [saved, setSaved] = useState(false);
  const saving = useAction(async () => {
    setSaved(false);
    await saveUplinkMacs(choiceOf(wifi, settings.wifiMac), choiceOf(ethernet, settings.ethernetMac));
    setSaved(true);
  });
  const fieldErrors = saving.fieldErrors;
  if (!settings.wifiUplinkAvailable && !settings.ethernetPresent) return null;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    saving.run();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
      <Section
        title="MAC address"
        footer="The address the networks RasBeacon joins see. Random hides the Pi from networks that track devices; cloned takes the place of a device the network already knows. A Wi-Fi network set to a random address above still gets its own."
      >
        {settings.wifiUplinkAvailable && (
          <UplinkMac
            label="Wi-Fi"
            draft={wifi}
            onChange={(draft) => {
              setWifi(draft);
              setSaved(false);
            }}
            saved={settings.wifiMac}
            clients={clients}
            error={fieldErrors.wifiMac}
          />
        )}
        {settings.ethernetPresent && (
          <UplinkMac
            label="Ethernet"
            draft={ethernet}
            onChange={(draft) => {
              setEthernet(draft);
              setSaved(false);
            }}
            saved={settings.ethernetMac}
            clients={clients}
            error={fieldErrors.ethernetMac}
          />
        )}
      </Section>
      {(wifi.mode !== '' || ethernet.mode !== '') && (
        <Notice tone="info">
          <p>Saving reconnects to the network with the new address. A hotel login page may ask you to log in again.</p>
        </Notice>
      )}
      <FormActions
        label="Save"
        busyLabel="Saving…"
        pending={saving.pending}
        error={saving.error}
        success={saved && 'Saved.'}
      />
    </form>
  );
}
