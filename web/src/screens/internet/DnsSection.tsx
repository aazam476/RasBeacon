import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { DNSSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { FormActions, Section, TextAreaField } from '@/ui';

/** The DNS servers devices use while their traffic isn't going through the VPN. */
export function DnsSection() {
  const { dnsSettings, reload } = useDevice();

  return (
    <Loaded data={dnsSettings} what="the DNS servers" onRetry={() => reload('dnsSettings')}>
      {(settings) => <DnsForm settings={settings} />}
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
    <form onSubmit={handleSubmit} noValidate>
      <Section
        title="DNS servers"
        footer="Used while your devices' traffic isn't going through the VPN; with the VPN on, its own DNS servers are used. Empty uses the DNS servers of the network RasBeacon is on, which hotel login pages need: while one is in the way, those are used anyway."
      >
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
      </Section>
    </form>
  );
}
