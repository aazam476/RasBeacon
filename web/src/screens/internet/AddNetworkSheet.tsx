import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { NearbyNetwork, NewNetworkForm, SavedNetwork, UplinkSecurity } from '@/data/types';
import { bandOfFrequency, formatDbm } from '@/lib/format';
import {
  BreakableText,
  Disclosure,
  FileField,
  FormSwitchRow,
  Notice,
  PasswordField,
  Row,
  Section,
  SelectField,
  Sheet,
  TextField,
} from '@/ui';
import { METHOD_OPTIONS, SECURITY_OPTIONS } from './networkDisplay';

/** Where the sheet was opened from. */
export type AddNetworkStart =
  /** A network found by a scan: its name and security are known. */
  | { from: 'scan'; network: NearbyNetwork & { security: UplinkSecurity } }
  /** "Add manually": for hidden networks or ones not in range now. */
  | { from: 'manual' };

const EMPTY_FORM: NewNetworkForm = {
  ssid: '',
  security: 'personal',
  password: '',
  hidden: false,
  method: 'ttls-pap',
  identity: '',
  anonymousIdentity: '',
  serverName: '',
  caCertFiles: [],
  clientCertFile: null,
  clientKeyFile: null,
  allowNoCertificateCheck: false,
};

function initialForm(start: AddNetworkStart): NewNetworkForm {
  switch (start.from) {
    case 'scan':
      return { ...EMPTY_FORM, ssid: start.network.ssid, security: start.network.security };
    case 'manual':
      return EMPTY_FORM;
  }
}

/** Dots where a password goes, as a hint (not a real value). */
const PASSWORD_PLACEHOLDER = '••••••••';

/** Typing settings for names and logins: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false } as const;

interface AddNetworkSheetProps {
  start: AddNetworkStart;
  /** The list as currently edited (the router refuses duplicates). */
  list: SavedNetwork[];
  /** Called with the new entry; it is saved together with the list. */
  onAdd: (network: SavedNetwork) => void;
  onClose: () => void;
}

/**
 * Adds a network to the list of networks RasBeacon joins. Enterprise networks (a company's or
 * a university's) get their login settings here, whether they were found by a scan or typed in.
 */
export function AddNetworkSheet({ start, list, onAdd, onClose }: AddNetworkSheetProps) {
  const { prepareNetwork } = useDevice();
  const [form, setForm] = useState(() => initialForm(start));
  // Inputs changed since the last attempt, whose old error messages no longer apply.
  const [changed, setChanged] = useState<ReadonlySet<string>>(new Set());

  function update<K extends keyof NewNetworkForm>(field: K, value: NewNetworkForm[K], errorKey: string = field) {
    setForm((current) => ({ ...current, [field]: value }));
    setChanged((names) => new Set([...names, errorKey]));
  }

  const fromScan = start.from === 'scan';

  return (
    <Sheet
      size={form.security === 'enterprise' ? 'lg' : 'md'}
      title="Add a Wi-Fi network"
      confirmLabel="Add to list"
      busyLabel="Adding…"
      onConfirm={async () => {
        setChanged(new Set());
        onAdd(await prepareNetwork(form, list));
      }}
      onClose={onClose}
    >
      {(allErrors) => {
        const errors: Record<string, string | undefined> = {};
        for (const [key, message] of Object.entries(allErrors)) if (!changed.has(key)) errors[key] = message;

        return (
          <>
            {fromScan ? (
              <Section level={3}>
                <Row
                  leading={{ tile: 'wifi', color: 'blue' }}
                  title={<BreakableText>{start.network.ssid}</BreakableText>}
                  subtitle={[
                    start.network.label,
                    bandOfFrequency(start.network.frequencyMhz),
                    formatDbm(start.network.signalDbm),
                  ].join(' · ')}
                />
                {form.security === 'personal' && (
                  <PasswordField
                    label="Network password"
                    placeholder={PASSWORD_PLACEHOLDER}
                    data-autofocus
                    value={form.password}
                    onChange={(password) => update('password', password)}
                    error={errors.password}
                    {...PLAIN_INPUT}
                  />
                )}
              </Section>
            ) : (
              <Section level={3}>
                <TextField
                  label="Network name"
                  placeholder="SSID"
                  data-autofocus
                  value={form.ssid}
                  onChange={(ssid) => update('ssid', ssid)}
                  error={errors.ssid}
                  {...PLAIN_INPUT}
                />
                <SelectField
                  label="Security"
                  options={SECURITY_OPTIONS}
                  value={form.security}
                  onChange={(security) => update('security', security)}
                />
                {form.security === 'personal' && (
                  <PasswordField
                    label="Network password"
                    placeholder={PASSWORD_PLACEHOLDER}
                    value={form.password}
                    onChange={(password) => update('password', password)}
                    error={errors.password}
                    {...PLAIN_INPUT}
                  />
                )}
                <FormSwitchRow
                  label="Hidden network"
                  labelNote="(doesn't show up in scans)"
                  checked={form.hidden}
                  onChange={(hidden) => update('hidden', hidden)}
                />
              </Section>
            )}
            {errors.ssid && fromScan && (
              <Notice tone="danger" role="alert">
                <p>{errors.ssid}</p>
              </Notice>
            )}

            {form.security === 'enterprise' && <EnterpriseFields form={form} update={update} errors={errors} />}
          </>
        );
      }}
    </Sheet>
  );
}

interface EnterpriseFieldsProps {
  form: NewNetworkForm;
  update: <K extends keyof NewNetworkForm>(field: K, value: NewNetworkForm[K], errorKey?: string) => void;
  errors: Record<string, string | undefined>;
}

/** The login settings of a WPA-Enterprise network (a company's or a university's, eduroam). */
function EnterpriseFields({ form, update, errors }: EnterpriseFieldsProps) {
  const tls = form.method === 'tls';

  return (
    <>
      <Section level={3} title="Enterprise login" footer="Your company or university publishes these settings.">
        <SelectField
          label="Login method"
          options={METHOD_OPTIONS}
          value={form.method}
          onChange={(method) => update('method', method)}
        />
        <TextField
          label="Username"
          inputMode="email"
          placeholder="user@example.com"
          value={form.identity}
          onChange={(identity) => update('identity', identity)}
          error={errors.identity}
          {...PLAIN_INPUT}
        />
        <TextField
          label="Anonymous identity"
          labelNote="(optional)"
          inputMode="email"
          placeholder="anonymous@example.com"
          value={form.anonymousIdentity}
          onChange={(anonymousIdentity) => update('anonymousIdentity', anonymousIdentity)}
          {...PLAIN_INPUT}
        />
        {tls ? (
          <>
            <FileField
              label="Your certificate file"
              accept=".pem,.crt,.cer,.der"
              onChange={(files) => update('clientCertFile', files[0] ?? null, 'clientCert')}
              error={errors.clientCert}
            />
            <FileField
              label="Your key file"
              accept=".pem,.key,.der"
              onChange={(files) => update('clientKeyFile', files[0] ?? null, 'clientKey')}
              error={errors.clientKey}
            />
            <PasswordField
              label="Key passphrase"
              labelNote="(if the key has one)"
              placeholder={PASSWORD_PLACEHOLDER}
              value={form.password}
              onChange={(password) => update('password', password)}
              error={errors.password}
              {...PLAIN_INPUT}
            />
          </>
        ) : (
          <PasswordField
            label="Your password"
            placeholder={PASSWORD_PLACEHOLDER}
            value={form.password}
            onChange={(password) => update('password', password)}
            error={errors.password}
            {...PLAIN_INPUT}
          />
        )}
      </Section>

      {/*
        Optional: without a CA certificate or a server name, RasBeacon trusts the login server it
        meets at the first connection, as phones do, and only that one afterwards.
      */}
      <Section level={3}>
        <Disclosure title="Advanced" defaultOpen={form.allowNoCertificateCheck} content="rows">
          <p className="row text-footnote text-label-2">
            Optional. Without these, RasBeacon trusts the login server it meets the first time, and only that one
            afterwards.
          </p>
          <FileField
            label="CA certificate file"
            labelNote="(choose several if there are more than one)"
            accept=".pem,.crt,.cer,.der"
            multiple
            onChange={(files) => update('caCertFiles', files, 'caCert')}
            error={errors.caCert}
          />
          <TextField
            label="Login server name"
            placeholder="radius.example.com"
            value={form.serverName}
            onChange={(serverName) => update('serverName', serverName)}
            error={errors.serverName}
            {...PLAIN_INPUT}
          />
          <FormSwitchRow
            label="Don't check the login server"
            labelNote="(unsafe)"
            checked={form.allowNoCertificateCheck}
            onChange={(allow) => update('allowNoCertificateCheck', allow)}
          />
          {form.allowNoCertificateCheck && (
            <div className="row">
              <Notice tone="danger" role="alert" title="The login server won't be checked" className="w-full">
                <p>
                  Without the check, anyone nearby can create a fake network with the same name and collect your
                  password. Only use this to test, and change your password afterwards.
                </p>
              </Notice>
            </div>
          )}
        </Disclosure>
      </Section>
    </>
  );
}
