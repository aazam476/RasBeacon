import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { ModemSettings, ModemState, ModemStatus } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { FormActions, Notice, PasswordField, Row, Section, TextField } from '@/ui';

/** What the modem is doing, in words. */
const STATE: Record<ModemState, string> = {
  connected: 'Connected',
  connecting: 'Connecting…',
  searching: 'Looking for a network',
  'pin-needed': 'Needs the SIM’s PIN',
  'pin-refused': 'The SIM refused the PIN',
  'puk-needed': 'SIM blocked',
  'no-sim': 'No SIM card',
  starting: 'Starting…',
  failed: 'Not working',
};

/** A notice for states the owner has to act on, or null. */
function ModemProblem({ modem }: { modem: ModemStatus }) {
  const tries =
    modem.pinTries >= 0 ? ` The SIM has ${modem.pinTries} ${modem.pinTries === 1 ? 'try' : 'tries'} left.` : '';
  switch (modem.state) {
    case 'pin-needed':
      return (
        <Notice tone="warning" title="Enter the SIM’s PIN">
          <p>The modem’s SIM card is locked. Enter its PIN below; RasBeacon tries it once.{tries}</p>
        </Notice>
      );
    case 'pin-refused':
      return (
        <Notice tone="danger" role="alert" title="The SIM refused the PIN">
          <p>
            RasBeacon won’t try that PIN again, so the SIM can’t be blocked by it. Check the PIN and save it again.
            {tries}
          </p>
        </Notice>
      );
    case 'puk-needed':
      return (
        <Notice tone="danger" role="alert" title="The SIM is blocked">
          <p>Too many wrong PINs were entered. Put the SIM in a phone and unblock it with its PUK code.</p>
        </Notice>
      );
    case 'no-sim':
      return (
        <Notice tone="warning" title="No SIM card">
          <p>Put a SIM card in the modem, then unplug it and plug it in again.</p>
        </Notice>
      );
    default:
      return null;
  }
}

/**
 * A 4G/5G modem that needs a modem manager (most M.2 and many 5G USB modems): its state, and
 * the carrier's settings to connect. Shown only when there is one.
 */
export function ModemSection({ modem, settings }: { modem: ModemStatus | undefined; settings: ModemSettings }) {
  const { saveModemSettings } = useDevice();
  const form = useForm({ ...settings, password: '', pin: '' }, saveModemSettings);
  const { values, setField, fieldErrors } = form;
  if (!modem) return null;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  const onNetwork = modem.state === 'connected' || modem.state === 'connecting';
  return (
    <>
      <ModemProblem modem={modem} />
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
        <Section
          title="Mobile modem"
          footer="Used for the internet when nothing else has it. Leave the APN empty to let the modem choose; your carrier lists it if it doesn’t connect."
        >
          <Row leading={{ tile: 'signal_wifi_4_bar', color: 'orange' }} title={modem.name} value={STATE[modem.state]} />
          {onNetwork && modem.operator && (
            <Row
              title="Network"
              value={[modem.operator, modem.network, `${modem.signalPercent}% signal`].filter(Boolean).join(', ')}
            />
          )}
          {modem.error && modem.state !== 'connected' && <Row title="Last problem" value={modem.error} />}
          <TextField
            label="APN"
            labelNote="(optional)"
            placeholder="internet"
            value={values.apn}
            onChange={(apn) => setField('apn', apn)}
            error={fieldErrors.apn}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
          />
          <TextField
            label="Username"
            labelNote="(optional)"
            value={values.username}
            onChange={(username) => setField('username', username)}
            error={fieldErrors.username}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
          />
          <PasswordField
            label="Password"
            labelNote={settings.hasPassword ? '(leave empty to keep it)' : '(optional)'}
            value={values.password ?? ''}
            onChange={(password) => setField('password', password)}
            error={fieldErrors.password}
            autoComplete="off"
          />
          <PasswordField
            label="SIM PIN"
            labelNote={settings.hasPin ? '(leave empty to keep it)' : '(if the SIM has one)'}
            inputMode="numeric"
            value={values.pin ?? ''}
            onChange={(pin) => setField('pin', pin)}
            error={fieldErrors.pin}
            autoComplete="off"
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
    </>
  );
}
