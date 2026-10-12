import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { GuestSettings, GuestStatus } from '@/data/types';
import { cn } from '@/lib/cn';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import {
  BreakableText,
  FormActions,
  FormError,
  FormSwitchRow,
  Icon,
  Notice,
  PasswordField,
  Row,
  Section,
  TextField,
  type BaseIconName,
} from '@/ui';

/** A second Wi-Fi network for visitors, kept apart from your devices. */
export function GuestsScreen() {
  const { guestSettings, status, reload } = useDevice();

  return (
    <Loaded data={guestSettings} what="the guest network settings" onRetry={() => reload('guestSettings')}>
      {(settings) => (
        <>
          {!settings.supported && (
            <Notice tone="warning" role="alert" title="This adapter can't run a guest network">
              <p>
                This Wi-Fi adapter can only broadcast one network, so it can&apos;t run a guest network. An adapter with
                a MediaTek chip (for example MT7612U) can.
              </p>
            </Notice>
          )}
          <GuestForm settings={settings} />
          {status.data?.guest.enabled && <GuestNow guest={status.data.guest} />}
          <Reach />
        </>
      )}
    </Loaded>
  );
}

/** Typing settings for names: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false } as const;

function GuestForm({ settings }: { settings: GuestSettings }) {
  const { saveGuestSettings } = useDevice();
  const form = useForm({ ...settings, password: '' }, saveGuestSettings);
  const { values, setField, fieldErrors } = form;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
      <Section footer="Visitors get their own Wi-Fi network and password.">
        <FormSwitchRow
          leading={{ tile: 'group', color: 'orange' }}
          label="Turn on the guest network"
          checked={values.enabled}
          // Without support it can only be turned off.
          disabled={!settings.supported && !values.enabled}
          onChange={(enabled) => setField('enabled', enabled)}
        >
          {fieldErrors.enabled && <FormError>{fieldErrors.enabled}</FormError>}
        </FormSwitchRow>
        <TextField
          label="Guest network name"
          value={values.ssid}
          onChange={(ssid) => setField('ssid', ssid)}
          error={fieldErrors.ssid}
          {...PLAIN_INPUT}
        />
        <PasswordField
          label={settings.hasPassword ? 'New guest password' : 'Guest password'}
          labelNote={settings.hasPassword ? '(leave empty to keep the current one)' : undefined}
          placeholder={settings.hasPassword ? 'Unchanged' : undefined}
          initiallyVisible
          value={values.password}
          onChange={(password) => setField('password', password)}
          error={fieldErrors.password}
          {...PLAIN_INPUT}
        />
      </Section>
      <FormActions
        label="Save"
        busyLabel="Saving…"
        pending={form.pending}
        error={form.error}
        success={form.saved && 'Saved. The hotspot has restarted.'}
        note="Saving restarts the hotspot: your devices disconnect for a few seconds."
      />
    </form>
  );
}

/** The guest network as it runs now. */
function GuestNow({ guest }: { guest: GuestStatus }) {
  return (
    <Section title="Now">
      <Row title="Network name" value={<BreakableText>{guest.ssid}</BreakableText>} />
      <Row title="State" value={guest.running ? 'Running' : 'Not running'} />
      <Row title="Guests connected" value={guest.clients.length} />
    </Section>
  );
}

const REACH: { icon: BaseIconName; text: string; allowed: boolean }[] = [
  { icon: 'public', text: 'The internet, through the VPN if it is on', allowed: true },
  { icon: 'devices', text: 'Your devices', allowed: false },
  { icon: 'group', text: 'Each other', allowed: false },
  { icon: 'settings', text: 'These settings', allowed: false },
];

/** What guests can and can't reach. */
function Reach() {
  return (
    <Section
      title="What guests can reach"
      footer="Guests can use the internet (through the VPN, if it is on), but can't reach your devices, each other, or these settings."
    >
      {REACH.map((item) => (
        <Row
          key={item.text}
          leading={{ icon: item.icon }}
          title={item.text}
          trailing={
            <span className={cn('flex items-center gap-1 text-subhead', item.allowed ? 'text-green' : 'text-red')}>
              <Icon name={item.allowed ? 'check' : 'block'} size={18} />
              {item.allowed ? 'Yes' : 'No'}
            </span>
          }
        />
      ))}
    </Section>
  );
}
