import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { DNSSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { FormActions, Section, TextField } from '@/ui';

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
  const form = useForm({ servers: settings.servers.join(', ') }, (values) => saveDnsSettings(values.servers));
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
        <TextField
          label="Servers"
          labelNote="(comma-separated; empty = the network's)"
          mono
          placeholder="1.1.1.1, 9.9.9.9"
          autoComplete="off"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
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
