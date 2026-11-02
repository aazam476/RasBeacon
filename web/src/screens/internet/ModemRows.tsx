import { useDevice } from '@/data/device';
import type { ModemSettings, ModemState, ModemStatus } from '@/data/types';
import { usePagePart } from '@/lib/pageForm';
import { useForm } from '@/lib/useForm';
import { Disclosure, Notice, PasswordField, Row, TextField } from '@/ui';
import { PART_ORDER } from './partOrder';

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
export function ModemProblem({ modem }: { modem: ModemStatus }) {
  const tries =
    modem.pinTries >= 0 ? ` The SIM has ${modem.pinTries} ${modem.pinTries === 1 ? 'try' : 'tries'} left.` : '';
  switch (modem.state) {
    case 'pin-needed':
      return (
        <Notice tone="warning" title="Enter the SIM’s PIN">
          <p>The modem’s SIM card is locked. Enter its PIN under Modem settings; RasBeacon tries it once.{tries}</p>
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

/** The modem's own rows: what it is doing, and the network it is on. */
export function ModemStatusRows({ modem }: { modem: ModemStatus }) {
  const onNetwork = modem.state === 'connected' || modem.state === 'connecting';
  return (
    <>
      <Row leading={{ tile: 'signal_wifi_4_bar', color: 'orange' }} title={modem.name} value={STATE[modem.state]} />
      {onNetwork && modem.operator && (
        <Row
          title="Network"
          value={[modem.operator, modem.network, `${modem.signalPercent}% signal`].filter(Boolean).join(', ')}
        />
      )}
      {modem.error && modem.state !== 'connected' && <Row title="Last problem" value={modem.error} />}
    </>
  );
}

/**
 * The carrier's settings a modem that needs a modem manager connects with (most M.2 and many 5G
 * USB modems), one level down: few people change them. Opens by itself when the SIM needs its
 * PIN. The page's Save saves them.
 */
export function ModemSettingsRows({ modem, settings }: { modem: ModemStatus; settings: ModemSettings }) {
  const { saveModemSettings } = useDevice();
  const form = useForm({ ...settings, password: '', pin: '' }, saveModemSettings);
  const { values, setField, fieldErrors } = form;
  usePagePart('modem', {
    order: PART_ORDER.modem,
    dirty: form.dirty,
    pending: form.pending,
    error: form.error,
    submit: form.submit,
    discard: form.discard,
    title: 'Modem settings',
  });
  const needsPin = modem.state === 'pin-needed' || modem.state === 'pin-refused';
  const hasError = Object.keys(fieldErrors).length > 0;

  return (
    <Disclosure
      title="Modem settings"
      subtitle="APN, username and SIM PIN"
      leading={{ icon: 'settings' }}
      content="rows"
      defaultOpen={needsPin || hasError}
    >
      <TextField
        label="APN"
        labelNote="(optional)"
        placeholder="internet"
        hint="Leave it empty to let the modem choose; your carrier lists it if it doesn’t connect."
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
    </Disclosure>
  );
}
