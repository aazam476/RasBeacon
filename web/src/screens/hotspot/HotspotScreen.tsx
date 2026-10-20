import { useState, type FormEvent } from 'react';
import { HOTSPOT_CHANNELS } from '@/data/constants';
import { useDevice } from '@/data/device';
import type { HotspotSettings, WiFiQR } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import {
  Badge,
  ButtonRow,
  FormActions,
  FormSwitchRow,
  PasswordField,
  Row,
  Section,
  SelectField,
  TextField,
} from '@/ui';
import { QrCode } from '../common/QrCode';
import { BAND_OPTIONS, HOTSPOT_SECURITY_OPTIONS } from '../common/wifiOptions';
import { AccessSection } from './AccessSection';

/** The settings that restart the hotspot when they change; only `metered` doesn't. */
const RESTARTING_FIELDS = ['ssid', 'band', 'channel', 'security', 'country'] as const;

/** Typing settings for names and codes: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false } as const;

/** RasBeacon's own Wi-Fi network: name, password, radio settings, the metered hint, and who can join. */
export function HotspotScreen() {
  const { hotspotSettings, reload } = useDevice();

  return (
    <Loaded data={hotspotSettings} what="the hotspot settings" onRetry={() => reload('hotspotSettings')}>
      {(settings) => (
        <>
          <HotspotForm settings={settings} />
          <CurrentPassword />
          <AccessSection />
        </>
      )}
    </Loaded>
  );
}

function HotspotForm({ settings }: { settings: HotspotSettings }) {
  const { status, saveHotspotSettings } = useDevice();
  const form = useForm({ ...settings, password: '' }, saveHotspotSettings);
  const { values, setField, fieldErrors } = form;
  const running = status.data?.hotspot.running;

  const restarts = values.password !== '' || RESTARTING_FIELDS.some((field) => values[field] !== settings[field]);
  const channelOptions = [
    { value: '0', label: 'Default' },
    ...HOTSPOT_CHANNELS[values.band].map((channel) => ({ value: String(channel), label: String(channel) })),
  ];

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
      <Section
        title="Network"
        action={
          running === undefined ? null : running ? (
            <Badge tone="green">Running</Badge>
          ) : (
            <Badge tone="red">Not running</Badge>
          )
        }
      >
        <TextField
          label="Network name"
          value={values.ssid}
          onChange={(ssid) => setField('ssid', ssid)}
          error={fieldErrors.ssid}
          {...PLAIN_INPUT}
        />
        <PasswordField
          label="New Wi-Fi password"
          labelNote="(leave empty to keep the current one)"
          placeholder="Unchanged"
          initiallyVisible
          value={values.password}
          onChange={(password) => setField('password', password)}
          error={fieldErrors.password}
          {...PLAIN_INPUT}
        />
      </Section>

      <Section
        title="Radio"
        footer={
          values.security === 'wpa3'
            ? "The country decides which channels are legal. With WPA3 only, devices older than about 2019 can't join."
            : 'The country decides which channels are legal.'
        }
      >
        <SelectField
          label="Band"
          options={BAND_OPTIONS}
          value={values.band}
          // A channel of the old band doesn't exist on the new one, so it goes back to the default.
          onChange={(band) => form.update((current) => ({ ...current, band, channel: 0 }), ['band', 'channel'])}
        />
        <SelectField
          label="Channel"
          options={channelOptions}
          value={String(values.channel)}
          onChange={(channel) => setField('channel', Number(channel))}
          error={fieldErrors.channel}
        />
        <SelectField
          label="Security"
          options={HOTSPOT_SECURITY_OPTIONS}
          value={values.security}
          onChange={(security) => setField('security', security)}
        />
        <TextField
          label="Country"
          labelNote="(two-letter code)"
          maxLength={2}
          value={values.country}
          onChange={(country) => setField('country', country.toUpperCase())}
          error={fieldErrors.country}
          {...PLAIN_INPUT}
          autoCapitalize="characters"
        />
      </Section>

      <Section
        footer={
          <>
            Asks devices to hold back large downloads and backups. Android reads this automatically when it connects. On
            iPhone and Mac, turn on Low Data Mode for this network in Wi-Fi settings; on Windows, set it as a metered
            connection. Changing only this doesn&apos;t restart the hotspot.
          </>
        }
      >
        <FormSwitchRow
          label="Metered connection"
          labelNote="(for example a phone's hotspot or a paid plan)"
          checked={values.metered}
          onChange={(metered) => setField('metered', metered)}
        />
      </Section>

      <FormActions
        label="Save"
        busyLabel="Saving…"
        pending={form.pending}
        error={form.error}
        success={form.saved && 'Saved.'}
        note={
          restarts || !form.dirty
            ? 'Saving restarts the hotspot: connected devices disconnect for a few seconds.'
            : 'Only the metered setting changed, so the hotspot keeps running.'
        }
      />
    </form>
  );
}

/**
 * Shows the hotspot's current password and a QR code to join it, hidden until asked for (it is
 * the password to your network). Being logged in is enough.
 */
function CurrentPassword() {
  const { revealHotspotPassword, wifiQr } = useDevice();
  const [revealed, setRevealed] = useState<{ password: string; qr: WiFiQR } | null>(null);

  if (revealed !== null) {
    return (
      <Section title="Join the hotspot" footer="Point a phone's camera at the code to join without typing.">
        <Row
          title={
            // One tap selects the whole password, ready to copy.
            <span className="selectable font-mono text-title3 break-all text-label select-all">
              {revealed.password}
            </span>
          }
        />
        <div className="row justify-center">
          <QrCode qr={revealed.qr} />
        </div>
        <ButtonRow onClick={() => setRevealed(null)}>Hide</ButtonRow>
      </Section>
    );
  }

  return (
    <Section title="Join the hotspot">
      <ButtonRow
        leading={{ icon: 'qr_code' }}
        action={async () => {
          const [password, qr] = await Promise.all([revealHotspotPassword(), wifiQr('hotspot')]);
          setRevealed({ password, qr });
        }}
        busyLabel="Loading…"
      >
        Show password and QR code
      </ButtonRow>
    </Section>
  );
}
