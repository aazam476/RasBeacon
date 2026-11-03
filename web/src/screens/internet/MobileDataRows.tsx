import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { MobileData } from '@/data/types';
import { formatBytes, ordinal, percentOf } from '@/lib/format';
import { DATA_UNITS, readDataLimit, showDataLimit } from '@/lib/limits';
import { usePagePart } from '@/lib/pageForm';
import { useForm } from '@/lib/useForm';
import { AlertDialog, AmountField, ButtonRow, Meter, RowText, SelectField } from '@/ui';
import { PART_ORDER } from './partOrder';

/** The days a month's count can start on: every month has a 28th. */
const MAX_RESET_DAY = 28;

/** The days of the month the count can start again on: "1st" to "28th". */
const DAYS = Array.from({ length: MAX_RESET_DAY }, (_, i) => ({ value: String(i + 1), label: ordinal(i + 1) }));

/**
 * The data phones and modems carried this month, as phone plans count it, with a monthly limit
 * that only warns (on the Status page) and the day the plan's month starts. Rows of the Phones
 * and modems section; the page's Save saves the limit and the day.
 */
export function MobileDataRows({ data }: { data: MobileData }) {
  const { saveMobileData, resetMobileData } = useDevice();
  // The limit as typed ("5" and GB), checked as it is typed; the router keeps MB.
  const form = useForm({ ...showDataLimit(data.limitMb), day: String(data.resetDay) }, (values) => {
    const typed = readDataLimit(values.text, values.unit);
    // Never reached with a mistake: the page's Save is off while there is one.
    return saveMobileData(typed.ok ? typed.value : data.limitMb, Number(values.day));
  });
  const { values, setField, fieldErrors } = form;
  const reading = readDataLimit(values.text, values.unit);
  const [resetting, setResetting] = useState(false);
  usePagePart('mobileData', {
    order: PART_ORDER.mobileData,
    dirty: form.dirty,
    pending: form.pending,
    error: form.error,
    invalid: !reading.ok,
    submit: form.submit,
    discard: form.discard,
    title: 'Phones and modems',
  });

  const limitBytes = data.limitMb * 1_000_000;
  const percent = limitBytes > 0 ? percentOf(data.usedBytes, limitBytes) : 0;
  const tone = percent >= 100 ? 'red' : percent >= 90 ? 'orange' : 'tint';
  const used = formatBytes(data.usedBytes);
  // The limit as the owner typed it: "5 GB", not "5.0 GB".
  const shownLimit = showDataLimit(data.limitMb);
  const limit = `${shownLimit.text} ${shownLimit.unit}`;

  return (
    <>
      <div className="row flex-col items-stretch gap-2.5 py-3">
        <RowText
          title="Data used this month"
          value={<span className="tabular">{limitBytes > 0 ? `${used} of ${limit}` : used}</span>}
        />
        {limitBytes > 0 && <Meter percent={percent} tone={tone} label="Mobile data used this month" />}
      </div>
      <AmountField
        label="Monthly limit"
        placeholder="No limit"
        value={values.text}
        onChange={(text) => setField('text', text)}
        unit={values.unit}
        units={DATA_UNITS}
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
    </>
  );
}
