import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { VPNDetailsForm, VPNProfile } from '@/data/types';
import { useForm } from '@/lib/useForm';
import {
  AlertDialog,
  ButtonRow,
  Disclosure,
  FormActions,
  FormSuccess,
  PasswordField,
  Row,
  Section,
  TextField,
} from '@/ui';

/** The form's text for a profile: lists joined with commas, numbers as text, keys empty (= keep). */
function toForm(profile: VPNProfile): VPNDetailsForm {
  return {
    endpoint: profile.endpoint,
    peerPublicKey: profile.peerPublicKey,
    addresses: profile.addresses.join(', '),
    dns: profile.dns.join(', '),
    allowedIps: profile.allowedIps.length > 0 ? profile.allowedIps.join(', ') : '0.0.0.0/0, ::/0',
    persistentKeepalive: profile.persistentKeepalive ? String(profile.persistentKeepalive) : '',
    mtu: profile.mtu ? String(profile.mtu) : '',
    routedIpv6Prefix: profile.routedIpv6Prefix,
    presharedKey: '',
    privateKey: '',
  };
}

/**
 * The server and key settings of the profile in use, tucked away for advanced users: importing
 * a file fills them in. Keys are write-only; only RasBeacon's public key is ever shown.
 */
export function ServerDetailsSection({ profile }: { profile: VPNProfile }) {
  return (
    <Section
      footer={
        <>
          For <span className="font-mono text-label">{profile.name}</span>. Importing a WireGuard file fills these in;
          change them only if your server&apos;s settings changed.
        </>
      }
    >
      <Disclosure title="Server details (advanced)" leading={{ icon: 'dns' }} content="rows">
        {/* A new profile starts with a fresh form. */}
        <ServerDetailsForm key={profile.name} profile={profile} />
      </Disclosure>
    </Section>
  );
}

/** Typing settings for keys and addresses: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false } as const;

function ServerDetailsForm({ profile }: { profile: VPNProfile }) {
  const { saveVpnDetails } = useDevice();
  const form = useForm(toForm(profile), (values) => saveVpnDetails(profile.name, values));
  const { values, setField, fieldErrors } = form;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  /** A text input of the form, connected to its value and error message. */
  const field = (name: keyof VPNDetailsForm) => ({
    value: values[name],
    onChange: (value: string) => setField(name, value),
    error: fieldErrors[name],
    ...PLAIN_INPUT,
  });

  return (
    <form onSubmit={handleSubmit} noValidate className="row-group">
      <TextField label="Server" labelNote="(host:port)" mono {...field('endpoint')} />
      <TextField label="Server's public key" mono {...field('peerPublicKey')} />
      <TextField label="RasBeacon's addresses in the tunnel" mono {...field('addresses')} />
      <TextField label="DNS servers" labelNote="(through the tunnel)" mono {...field('dns')} />
      <TextField label="Allowed IPs" labelNote="(must include 0.0.0.0/0)" mono {...field('allowedIps')} />
      <TextField
        label="Keepalive"
        labelNote="(seconds, empty = 25)"
        inputMode="numeric"
        placeholder="25"
        {...field('persistentKeepalive')}
      />
      <TextField label="MTU" labelNote="(empty = 1380)" inputMode="numeric" placeholder="1380" {...field('mtu')} />
      <TextField
        label="Public IPv6 network"
        labelNote="(optional)"
        mono
        placeholder="2001:db8:1234:5678::/64"
        hint="Only if your WireGuard server routes a public IPv6 /64 to RasBeacon: devices then get their own public IPv6 addresses through the VPN, without NAT. Leave it empty otherwise."
        {...field('routedIpv6Prefix')}
      />
      <PasswordField
        label="New preshared key"
        labelNote={profile.hasPresharedKey ? '(optional; one is saved)' : '(optional)'}
        placeholder={profile.hasPresharedKey ? 'Unchanged' : 'None'}
        {...field('presharedKey')}
      />
      <PasswordField
        label="New private key"
        labelNote="(empty = keep)"
        placeholder="Unchanged"
        {...field('privateKey')}
      />
      <div className="row">
        <FormActions
          inRow
          className="w-full"
          label="Save"
          busyLabel="Saving…"
          pending={form.pending}
          error={form.error}
          success={form.saved && 'Saved.'}
        />
      </div>
    </form>
  );
}

/** RasBeacon's public key for the profile in use, to give to the WireGuard server, and making a new one. */
export function PublicKeySection({ profile }: { profile: VPNProfile }) {
  const { generateVpnKey } = useDevice();
  const [confirming, setConfirming] = useState(false);
  const [madeNew, setMadeNew] = useState(false);

  return (
    <Section title="RasBeacon's public key" footer="Give this to your WireGuard server.">
      <Row
        title={
          // One tap selects the whole key, ready to copy.
          <span className="selectable font-mono text-callout break-all select-all">
            {profile.publicKey || 'none yet'}
          </span>
        }
      />
      {madeNew && (
        <div className="row">
          <FormSuccess>New key pair made. Give the public key above to your WireGuard server.</FormSuccess>
        </div>
      )}
      <ButtonRow tone="danger" onClick={() => setConfirming(true)}>
        Generate new key
      </ButtonRow>
      {confirming && (
        <AlertDialog
          title="Generate a new key?"
          tone="danger"
          confirmLabel="Generate"
          busyLabel="Generating…"
          onConfirm={async () => {
            await generateVpnKey(profile.name);
            setMadeNew(true);
          }}
          onClose={() => setConfirming(false)}
        >
          <p>
            Make a new key pair for <span className="font-mono wrap-anywhere">{profile.name}</span>? The WireGuard
            server must then be given RasBeacon&apos;s new public key, or the VPN stops working.
          </p>
        </AlertDialog>
      )}
    </Section>
  );
}
