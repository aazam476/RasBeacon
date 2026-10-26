import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { AccessSettings, Client } from '@/data/types';
import { readDeviceLimit } from '@/lib/limits';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import {
  AlertDialog,
  Badge,
  BreakableText,
  Button,
  Disclosure,
  FormActions,
  FormSwitchRow,
  Notice,
  Row,
  Section,
  TextAreaField,
  TextField,
} from '@/ui';
import { clientName } from '../common/clientDisplay';

/**
 * Who may join over Wi-Fi: only listed devices, and how many at once. Few people need either, so
 * both are one level down. Devices on Ethernet always get in.
 */
export function AccessSection() {
  const { accessSettings, reload } = useDevice();

  return (
    <Loaded data={accessSettings} what="who can join" onRetry={() => reload('accessSettings')}>
      {(settings) => (
        <Section
          title="Who can join"
          footer="Only Wi-Fi devices are checked; devices on Ethernet always get in. Saving restarts the hotspot."
        >
          <Disclosure
            title="Allowed devices"
            subtitle={settings.allowListOn ? countDevices(settings.allowed.length) : 'Anyone with the password'}
            leading={{ icon: 'verified_user' }}
            content="rows"
          >
            <AllowListForm settings={settings} />
          </Disclosure>
          <Disclosure
            title="Device limit"
            subtitle={settings.maxDevices > 0 ? countDevices(settings.maxDevices) : 'No limit'}
            leading={{ icon: 'devices' }}
            content="rows"
          >
            <LimitForm settings={settings} />
          </Disclosure>
        </Section>
      )}
    </Loaded>
  );
}

function AllowListForm({ settings }: { settings: AccessSettings }) {
  const { status, saveAccessSettings } = useDevice();
  const form = useForm({ on: settings.allowListOn, list: settings.allowed.join('\n') }, (values) =>
    saveAccessSettings({ ...settings, allowListOn: values.on, allowed: parseList(values.list) }),
  );
  const { values, setField, fieldErrors } = form;
  const [confirming, setConfirming] = useState(false);

  const listed = new Set(parseList(values.list).map(normalize));
  const wifiClients = (status.data?.clients ?? []).filter((client) => !client.wired);
  const thisDevice = wifiClients.find((client) => client.isThisDevice);
  const locksMeOut = values.on && thisDevice !== undefined && !listed.has(normalize(thisDevice.mac));

  function add(client: Client) {
    setField('list', [values.list.trim(), client.mac].filter(Boolean).join('\n'));
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (locksMeOut) setConfirming(true);
    else form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="row-group">
      <FormSwitchRow
        label="Only allow these devices"
        description="Other devices can't join your network, even with the password. The guest network isn't affected."
        checked={values.on}
        onChange={(on) => setField('on', on)}
      />
      <TextAreaField
        label="MAC addresses"
        labelNote="(one per line)"
        hint="Phones and laptops often use a private address for each network: on the device, find the one it uses for this network in its Wi-Fi settings."
        mono
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        rows={Math.min(8, Math.max(3, listed.size + 1))}
        placeholder={'3a:1f:22:aa:bb:cc\n7e:04:91:5d:20:e8'}
        value={values.list}
        onChange={(list) => setField('list', list)}
        error={fieldErrors.allowed}
      />
      {locksMeOut && (
        <div className="row">
          <Notice
            tone="warning"
            title="This device isn't on the list"
            actions={
              <Button size="sm" variant="secondary" icon="add" onClick={() => add(thisDevice)}>
                Add this device
              </Button>
            }
          >
            After saving, it can&apos;t join over Wi-Fi again and you&apos;d lose this page.
          </Notice>
        </div>
      )}
      {wifiClients.length > 0 && (
        <p className="row pb-0 text-footnote font-semibold text-label-2">On your network now</p>
      )}
      {wifiClients.map((client) => (
        <Row
          key={client.mac}
          title={
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <BreakableText>{clientName(client)}</BreakableText>
              {client.isThisDevice && <Badge tone="tint">This device</Badge>}
            </span>
          }
          subtitle={<span className="font-mono wrap-anywhere">{client.mac}</span>}
          trailing={
            listed.has(normalize(client.mac)) ? (
              <span className="text-subhead text-label-2">Added</span>
            ) : (
              <Button size="sm" variant="secondary" icon="add" onClick={() => add(client)}>
                Add
              </Button>
            )
          }
        />
      ))}
      <div className="row">
        <FormActions
          inRow
          className="w-full"
          label="Save"
          busyLabel="Restarting the hotspot…"
          pending={form.pending}
          error={form.error}
          success={form.saved && 'Saved.'}
        />
      </div>
      {confirming && (
        <AlertDialog
          title="Lock out this device?"
          tone="danger"
          confirmLabel="Save anyway"
          busyLabel="Saving…"
          onConfirm={async () => void (await form.submit())}
          onClose={() => setConfirming(false)}
        >
          <p>This device isn&apos;t on the list, so it can&apos;t rejoin over Wi-Fi after the hotspot restarts.</p>
        </AlertDialog>
      )}
    </form>
  );
}

function LimitForm({ settings }: { settings: AccessSettings }) {
  const { saveAccessSettings } = useDevice();
  // Empty means no limit; the number is checked as it is typed.
  const form = useForm({ maxDevices: settings.maxDevices > 0 ? String(settings.maxDevices) : '' }, (values) => {
    const typed = readDeviceLimit(values.maxDevices);
    // Never reached with a mistake: Save is off while there is one (see handleSubmit).
    return saveAccessSettings({ ...settings, maxDevices: typed.ok ? typed.value : settings.maxDevices });
  });
  const { values, setField, fieldErrors } = form;
  const reading = readDeviceLimit(values.maxDevices);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (reading.ok) form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="row-group">
      <TextField
        label="At most"
        labelNote="(devices)"
        placeholder="No limit"
        inputMode="numeric"
        autoComplete="off"
        hint="On your network at once. The guest network has its own limit of the same number."
        value={values.maxDevices}
        onChange={(maxDevices) => setField('maxDevices', maxDevices)}
        error={(reading.ok ? undefined : reading.error) ?? fieldErrors.maxDevices}
      />
      <div className="row">
        <FormActions
          inRow
          className="w-full"
          label="Save"
          busyLabel="Restarting the hotspot…"
          pending={form.pending}
          error={form.error}
          success={form.saved && 'Saved.'}
          disabled={!reading.ok}
        />
      </div>
    </form>
  );
}

function countDevices(count: number): string {
  return count === 1 ? '1 device' : `${count} devices`;
}

/** The non-empty lines of the list. The router checks and tidies each address. */
function parseList(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean);
}

/** For comparing addresses typed in any common form. */
function normalize(mac: string): string {
  return mac.toLowerCase().replaceAll('-', ':');
}
