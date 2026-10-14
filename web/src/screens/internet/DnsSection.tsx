import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { DNSSettings } from '@/data/types';
import { keepTogether } from '@/lib/format';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { Disclosure, FormActions, FormSwitchRow, Section, TextAreaField } from '@/ui';

/**
 * The DNS servers devices use: while their traffic isn't going through the VPN, and, with the
 * switch on, while it is (then reached through the VPN). Few people change them, so they are one
 * level down, with the servers in use shown on the row.
 */
export function DnsSection() {
  const { dnsSettings, reload } = useDevice();

  return (
    <Loaded data={dnsSettings} what="the DNS servers" onRetry={() => reload('dnsSettings')}>
      {(settings) => (
        <Section
          footer={
            settings.withVpn && settings.servers.length > 0
              ? "For your devices, also while the VPN is on (then through the VPN). Hotel login pages always use the network's own servers."
              : "For your devices while the VPN is off; the VPN brings its own. Hotel login pages always use the network's own servers."
          }
        >
          <Disclosure
            title="DNS servers"
            subtitle={
              settings.servers.length > 0 ? settings.servers.map(serverSummary).join(', ') : "The network's own"
            }
            leading={{ icon: 'dns' }}
            content="rows"
          >
            <DnsForm settings={settings} />
          </Disclosure>
        </Section>
      )}
    </Loaded>
  );
}

function DnsForm({ settings }: { settings: DNSSettings }) {
  const { saveDnsSettings } = useDevice();
  const form = useForm({ servers: settings.servers.join('\n'), withVpn: settings.withVpn }, (values) =>
    saveDnsSettings(values.servers, values.withVpn),
  );
  const { values, setField, fieldErrors } = form;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="row-group">
      <TextAreaField
        label="Servers"
        labelNote="(up to 4, one per line; empty = the network's)"
        hint="An address like 1.1.1.1, or a DNS-over-HTTPS server like https://dns.google/dns-query, which keeps the network from seeing or changing your lookups. Its name is found with the addresses in the list, or else the network's DNS."
        mono
        rows={rowsFor(settings.servers)}
        placeholder={'https://dns.google/dns-query\n1.1.1.1'}
        value={values.servers}
        onChange={(servers) => setField('servers', servers)}
        error={fieldErrors.servers}
      />
      <FormSwitchRow
        label="Also use while the VPN is on"
        description="Lookups go through the VPN to these servers instead of the VPN's. Turn on for a server on RasBeacon, like AdGuard Home."
        checked={values.withVpn}
        onChange={(withVpn) => setField('withVpn', withVpn)}
        disabled={values.servers.trim() === ''}
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

/** "1.1.1.1", or a DoH server's host: "dns.google (encrypted)". */
function serverSummary(server: string): string {
  if (!server.startsWith('https://')) return server;
  try {
    return keepTogether(`${new URL(server).host} (encrypted)`);
  } catch {
    return server;
  }
}

/**
 * Enough rows to show the saved servers without scrolling on a phone, where a long DoH address
 * wraps (about 28 characters a line): from 2 to 7.
 */
function rowsFor(servers: string[]): number {
  const lines = servers.reduce((sum, server) => sum + Math.ceil(server.length / 28), 0);
  return Math.min(7, Math.max(2, lines));
}
