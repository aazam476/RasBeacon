import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { MobileData } from '@/data/types';
import { formatBytes, ordinal, percentOf } from '@/lib/format';
import { readDataLimit, showDataLimit, type DataUnit } from '@/lib/limits';
import { useForm } from '@/lib/useForm';
import { AlertDialog, AmountField, ButtonRow, FormActions, Meter, RowText, Section, SelectField } from '@/ui';

const UNITS: readonly DataUnit[] = ['GB', 'MB'];

/** The days a month's count can start on: every month has a 28th. */
const MAX_RESET_DAY = 28;

const DAYS = Array.from({ length: MAX_RESET_DAY }, (_, i) => ({ value: String(i + 1), label: ordinal(i + 1) }));

/** "October 1" (in the browser's language and order). */
function sinceLabel(since: string | undefined): string {
  const date = since ? new Date(since) : null;
  if (!date || Number.isNaN(date.getTime())) return 'the start of the month';
  return date.toLocaleDateString(undefined, { day: 'numeric', month: 'long', timeZone: 'UTC' });
}

/**
 * The data phones and modems carried this month, as phone plans count it, with a monthly limit
 * that only warns (on the Status page). Shown while a phone or modem is plugged in, or once one
 * was used this month.
 */
export function MobileDataSection({ data }: { data: MobileData | undefined }) {
  if (!data) return null;
  return <MobileDataForm data={data} />;
}

function MobileDataForm({ data }: { data: MobileData }) {
  const { saveMobileData, resetMobileData } = useDevice();
  // The limit as typed ("5" and GB), checked as it is typed; the router keeps MB.
  const form = useForm({ ...showDataLimit(data.limitMb), day: String(data.resetDay) }, (values) => {
    const typed = readDataLimit(values.text, values.unit);
    // Never reached with a mistake: Save is off while there is one (see handleSubmit).
    return saveMobileData(typed.ok ? typed.value : data.limitMb, Number(values.day));
  });
  const { values, setField, fieldErrors } = form;
  const reading = readDataLimit(values.text, values.unit);
  const [resetting, setResetting] = useState(false);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (reading.ok) form.submit();
  }

  const limitBytes = data.limitMb * 1_000_000;
  const percent = limitBytes > 0 ? percentOf(data.usedBytes, limitBytes) : 0;
  const tone = percent >= 100 ? 'red' : percent >= 90 ? 'orange' : 'tint';
  const used = formatBytes(data.usedBytes);
  // The limit as the owner typed it: "5 GB", not "5.0 GB".
  const shownLimit = showDataLimit(data.limitMb);
  const limit = `${shownLimit.text}\u00a0${shownLimit.unit}`;

  return (
    <Section
      title="Mobile data"
      footer={`Counted for phones and modems since ${sinceLabel(data.since)}. The Status page warns you as you near the limit. Nothing is blocked.`}
    >
      <div className="row flex-col items-stretch gap-2.5 py-3">
        <RowText
          title="Used this month"
          value={<span className="tabular">{limitBytes > 0 ? `${used} of ${limit}` : used}</span>}
        />
        {limitBytes > 0 && <Meter percent={percent} tone={tone} label="Mobile data used this month" />}
      </div>
      <form onSubmit={handleSubmit} noValidate className="row-group">
        <AmountField
          label="Monthly limit"
          placeholder="No limit"
          value={values.text}
          onChange={(text) => setField('text', text)}
          unit={values.unit}
          units={UNITS}
          onUnitChange={(unit) => setField('unit', unit)}
          error={(reading.ok ? undefined : reading.error) ?? fieldErrors.limitMb}
          disabled={form.pending}
        />
        <SelectField
          label="Month starts on the"
          options={DAYS}
          value={values.day}
          onChange={(day) => setField('day', day)}
          error={fieldErrors.resetDay}
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
        Reset count
      </ButtonRow>
      {resetting && (
        <AlertDialog
          title="Reset the count?"
          tone="danger"
          confirmLabel="Reset"
          busyLabel="Resetting…"
          onConfirm={resetMobileData}
          onClose={() => setResetting(false)}
        >
          <p>This month's count starts again from zero, for example after a new SIM card or a top-up.</p>
        </AlertDialog>
      )}
    </Section>
  );
}
