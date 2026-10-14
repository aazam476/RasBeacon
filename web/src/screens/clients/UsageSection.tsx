import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { UsageSettings } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { AlertDialog, ButtonRow, FormActions, Section, SelectField } from '@/ui';

/** The limits to choose from, in MB (decimal, as the counts are shown). */
const LIMITS_MB = [0, 500, 1_000, 2_000, 5_000, 10_000, 20_000, 50_000, 100_000];

/** "No limit", "500 MB", "2 GB". */
function limitLabel(limitMb: number): string {
  return limitMb === 0 ? 'No limit' : formatBytes(limitMb * 1_000_000);
}

/** "1 October", or with the year when it isn't this year. */
function sinceLabel(since: string | undefined): string {
  const date = since ? new Date(since) : null;
  if (!date || Number.isNaN(date.getTime())) return 'the start';
  const sameYear = date.getFullYear() === new Date().getFullYear();
  return date.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: sameYear ? undefined : 'numeric' });
}

/**
 * Data use: counted for each Wi-Fi device until the counts are reset, with a limit per device
 * that pauses it while the connection is marked metered.
 */
export function UsageSection() {
  const { usageSettings, reload } = useDevice();

  return (
    <Loaded data={usageSettings} what="the data limit" onRetry={() => reload('usageSettings')}>
      {(settings) => <UsageForm settings={settings} />}
    </Loaded>
  );
}

function UsageForm({ settings }: { settings: UsageSettings }) {
  const { saveUsageLimit, resetUsage } = useDevice();
  const form = useForm({ limitMb: String(settings.limitMb) }, (values) => saveUsageLimit(Number(values.limitMb)));
  const { values, setField, fieldErrors } = form;
  const [resetting, setResetting] = useState(false);
  // A limit set some other way stays in the menu.
  const limits = LIMITS_MB.includes(settings.limitMb)
    ? LIMITS_MB
    : [...LIMITS_MB, settings.limitMb].sort((a, b) => a - b);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  let footer = `Counted for each Wi-Fi device since ${sinceLabel(settings.since)}. `;
  if (settings.limitMb === 0) {
    footer += 'A limit pauses a device that uses it up, while the hotspot is set as a metered connection.';
  } else if (!settings.metered) {
    footer += 'The limit is not in use: it only applies while the hotspot is set as a metered connection.';
  } else {
    footer +=
      settings.devicesPaused > 0
        ? `${settings.devicesPaused === 1 ? 'One device has' : `${settings.devicesPaused} devices have`} used the limit and ${settings.devicesPaused === 1 ? 'is' : 'are'} paused until you reset the counts or raise the limit.`
        : 'A device that uses it up is paused until you reset the counts or raise the limit.';
  }

  return (
    <Section title="Data use" footer={footer}>
      <form onSubmit={handleSubmit} noValidate className="row-group">
        <SelectField
          label="Limit per device"
          options={limits.map((limitMb) => ({ value: String(limitMb), label: limitLabel(limitMb) }))}
          value={values.limitMb}
          onChange={(limitMb) => setField('limitMb', limitMb)}
          error={fieldErrors.limitMb}
          disabled={form.pending}
        />
        {form.dirty && (
          <div className="row">
            <FormActions
              inRow
              className="w-full"
              label="Save"
              busyLabel="Saving…"
              pending={form.pending}
              error={form.error}
            />
          </div>
        )}
      </form>
      <ButtonRow tone="danger" onClick={() => setResetting(true)}>
        Reset counts
      </ButtonRow>
      {resetting && (
        <AlertDialog
          title="Reset the counts?"
          tone="danger"
          confirmLabel="Reset"
          busyLabel="Resetting…"
          onConfirm={resetUsage}
          onClose={() => setResetting(false)}
        >
          <p>Every device starts again from zero, and devices paused by the limit can use the internet again.</p>
        </AlertDialog>
      )}
    </Section>
  );
}
