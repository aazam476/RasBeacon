import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { DNSSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { Disclosure, FormActions, Section, TextAreaField } from '@/ui';

/**
 * The DNS servers devices use while their traffic isn't going through the VPN. Few people change
 * them, so they are one level down, with the servers in use shown on the row.
 */
export function DnsSection() {
  const { dnsSettings, reload } = useDevice();

  return (
    <Loaded data={dnsSettings} what="the DNS servers" onRetry={() => reload('dnsSettings')}>
      {(settings) => (
        <Section footer="For your devices while the VPN is off; the VPN brings its own. Hotel login pages always use the network's own servers.">
          <Disclosure
            title="DNS servers"
            subtitle={settings.servers.length > 0 ? settings.servers.join(', ') : "The network's own"}
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
  const form = useForm({ servers: settings.servers.join('\n') }, (values) => saveDnsSettings(values.servers));
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
        mono
        rows={settings.servers.length > 2 ? 4 : 2}
        placeholder={'1.1.1.1\n9.9.9.9'}
        value={values.servers}
        onChange={(servers) => setField('servers', servers)}
        error={fieldErrors.servers}
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
