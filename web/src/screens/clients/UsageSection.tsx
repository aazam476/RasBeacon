import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { UsageSettings } from '@/data/types';
import { readDataLimit, showDataLimit, type DataUnit } from '@/lib/limits';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { AlertDialog, AmountField, ButtonRow, FormActions, Section } from '@/ui';

const UNITS: readonly DataUnit[] = ['GB', 'MB'];

/** "October 19, 2026" (in the browser's language and order). */
function sinceLabel(since: string | undefined): string {
  const date = since ? new Date(since) : null;
  if (!date || Number.isNaN(date.getTime())) return 'the start';
  return date.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' });
}

/**
 * Data use: counted for each Wi-Fi device until the counts are reset, with a limit per device
 * that blocks it once used up.
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
  // The limit as typed ("5" and GB), checked as it is typed; the router keeps MB.
  const form = useForm(showDataLimit(settings.limitMb), (values) => {
    const typed = readDataLimit(values.text, values.unit);
    // Never reached with a mistake: Save is off while there is one (see handleSubmit).
    return saveUsageLimit(typed.ok ? typed.value : settings.limitMb);
  });
  const { values, setField, fieldErrors } = form;
  const reading = readDataLimit(values.text, values.unit);
  const [resetting, setResetting] = useState(false);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (reading.ok) form.submit();
  }

  let footer = `Counted for each Wi-Fi device since ${sinceLabel(settings.since)}. `;
  footer +=
    settings.limitMb > 0 && settings.devicesPaused > 0
      ? `${settings.devicesPaused === 1 ? 'One device has' : `${settings.devicesPaused} devices have`} used up ${settings.devicesPaused === 1 ? 'its' : 'their'} data, so ${settings.devicesPaused === 1 ? 'its' : 'their'} traffic is blocked until you reset the counts or raise the limit.`
      : 'When a device uses up its data, its traffic is blocked until you reset the counts or raise the limit.';

  return (
    <Section title="Data use" footer={footer}>
      <form onSubmit={handleSubmit} noValidate className="row-group">
        <AmountField
          label="Limit per device"
          placeholder="No limit"
          value={values.text}
          onChange={(text) => setField('text', text)}
          unit={values.unit}
          units={UNITS}
          onUnitChange={(unit) => setField('unit', unit)}
          error={(reading.ok ? undefined : reading.error) ?? fieldErrors.limitMb}
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
              disabled={!reading.ok}
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
