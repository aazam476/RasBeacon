import { useEffect, useState, type FormEvent } from 'react';
import { HOTSPOT_CHANNELS } from '@/data/constants';
import { useDevice } from '@/data/device';
import { FieldErrors } from '@/data/errors';
import type { ChannelsByBand, HotspotSettings, WiFiQR } from '@/data/types';
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
import { bandOptions, securityOptions } from '../common/wifiOptions';
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

/**
 * The channels the router allows for `country` (its regulatory database), asked for again once a
 * full two-letter code is typed. Until it answers, or if it can't, every channel RasBeacon offers;
 * a code that isn't a country comes back as `countryError`, shown under the field at once.
 */
function useCountryChannels(country: string, no5GHz?: boolean): { channels: ChannelsByBand; countryError?: string } {
  const { hotspotChannels } = useDevice();
  const [answer, setAnswer] = useState<{ country: string; channels?: ChannelsByBand; error?: string } | null>(null);
  useEffect(() => {
    if (!/^[A-Z]{2}$/.test(country)) return;
    let current = true; // a later country's answer wins, whichever arrives first
    hotspotChannels(country).then(
      (channels) => {
        if (current && channels) setAnswer({ country, channels });
      },
      (problem: unknown) => {
        if (current) setAnswer({ country, error: countryProblem(problem) });
      },
    );
    return () => {
      current = false;
    };
  }, [country, hotspotChannels]);

  const offered = { '2.4GHz': [...HOTSPOT_CHANNELS['2.4GHz']], '5GHz': no5GHz ? [] : [...HOTSPOT_CHANNELS['5GHz']] };
  if (answer?.country !== country) return { channels: offered };
  return { channels: answer.channels ?? offered, countryError: answer.error };
}

/** The router's reason a country code was refused ("Use GB for the United Kingdom."). */
function countryProblem(problem: unknown): string | undefined {
  if (problem instanceof FieldErrors) return problem.fields.country;
  return undefined; // RasBeacon didn't answer: nothing to say about the code, and every channel stays
}

function HotspotForm({ settings }: { settings: HotspotSettings }) {
  const { status, saveHotspotSettings } = useDevice();
  // A hotspot saved on 5 GHz whose radio can't use it runs on 2.4 GHz (after the adapter was
  // swapped for another), so the form starts there.
  const band = settings.no5GHz ? '2.4GHz' : settings.band;
  // Likewise, "WPA3 only" on a radio that can't run it runs as WPA2/WPA3.
  const security = settings.noWpa3 && settings.security === 'wpa3' ? 'wpa2-wpa3' : settings.security;
  const form = useForm({ ...settings, band, security, password: '' }, saveHotspotSettings);
  const { values, setField, fieldErrors } = form;
  const running = status.data?.hotspot.running;

  const restarts = values.password !== '' || RESTARTING_FIELDS.some((field) => values[field] !== settings[field]);
  const allowed = useCountryChannels(values.country, settings.no5GHz);
  const channelOptions = [
    { value: '0', label: 'Default' },
    ...allowed.channels[values.band].map((channel) => ({ value: String(channel), label: String(channel) })),
  ];
  // A channel picked before the country changed stays shown, marked, until another is picked.
  const channelRefused = values.channel !== 0 && !allowed.channels[values.band].includes(values.channel);
  if (channelRefused) {
    channelOptions.push({ value: String(values.channel), label: `${values.channel} (not allowed here)` });
  }
  const bandRefused = values.band === '5GHz' && allowed.channels['5GHz'].length === 0;

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
        footer={[
          'The country decides which channels are legal.',
          values.security === 'wpa3' && "With WPA3 only, devices older than about 2019 can't join.",
          settings.no5GHz && "This Wi-Fi adapter can't use 5 GHz.",
          settings.noWpa3 && "This Wi-Fi adapter can't run WPA3 only.",
        ]
          .filter(Boolean)
          .join(' ')}
      >
        <TextField
          label="Country"
          labelNote="(two-letter code)"
          maxLength={2}
          value={values.country}
          onChange={(country) => setField('country', country.toUpperCase())}
          error={fieldErrors.country ?? allowed.countryError}
          {...PLAIN_INPUT}
          autoCapitalize="characters"
        />
        <SelectField
          label="Band"
          options={bandOptions(settings.no5GHz || (allowed.channels['5GHz'].length === 0 && values.band !== '5GHz'))}
          value={values.band}
          error={fieldErrors.band ?? (bandRefused ? "This country doesn't allow a hotspot on 5 GHz." : undefined)}
          // A channel of the old band doesn't exist on the new one, so it goes back to the default.
          onChange={(band) => form.update((current) => ({ ...current, band, channel: 0 }), ['band', 'channel'])}
        />
        <SelectField
          label="Channel"
          options={channelOptions}
          value={String(values.channel)}
          onChange={(channel) => setField('channel', Number(channel))}
          error={fieldErrors.channel ?? (channelRefused ? "This channel isn't allowed in this country." : undefined)}
        />
        <SelectField
          label="Security"
          options={securityOptions(settings.noWpa3)}
          value={values.security}
          onChange={(security) => setField('security', security)}
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
