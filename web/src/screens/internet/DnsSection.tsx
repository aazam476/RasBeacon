import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { DNSSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { Disclosure, FormActions, FormSwitchRow, Section, TextAreaField } from '@/ui';

/**
 * The DNS servers devices use: while their traffic isn't going through the VPN, and, with the
 * switch on, while it is (then reached through the VPN). Few people change them, so they are one
 * level down; the list shows when the row is opened.
 */
export function DnsSection() {
  const { dnsSettings, reload } = useDevice();

  return (
    <Loaded data={dnsSettings} what="the DNS servers" onRetry={() => reload('dnsSettings')}>
      {(settings) => (
        <Section>
          <Disclosure title="DNS servers" leading={{ icon: 'dns' }} content="rows">
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
        label="DNS servers"
        labelNote="(up to 4, one per line)"
        hint="Enter DNS or DNS-over-HTTPS addresses, such as 1.1.1.1 or https://cloudflare-dns.com/dns-query. Your devices' lookups go to the first server, then to the next if it doesn't answer. Leave empty to use the network's DNS servers. Login pages, such as a hotel's, always use the network's DNS."
        mono
        rows={rowsFor(settings.servers)}
        placeholder={'https://cloudflare-dns.com/dns-query\n1.1.1.1'}
        value={values.servers}
        onChange={(servers) => setField('servers', servers)}
        error={fieldErrors.servers}
      />
      <FormSwitchRow
        label="Use these servers while the VPN is on"
        description="Overrides the DNS servers in the WireGuard profile."
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

/**
 * Enough rows to show the saved servers without scrolling on a phone, where a long DoH address
 * wraps (about 28 characters a line): from 2 to 7.
 */
function rowsFor(servers: string[]): number {
  const lines = servers.reduce((sum, server) => sum + Math.ceil(server.length / 28), 0);
  return Math.min(7, Math.max(2, lines));
}
