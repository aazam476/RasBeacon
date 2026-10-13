import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { SwapSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { FormActions, Section, SelectField } from '@/ui';

/** The swap file's size: room on the SD card for memory the Pi isn't using. */
export function SwapSection() {
  const { swapSettings, reload } = useDevice();

  return (
    <Loaded data={swapSettings} what="the swap settings" onRetry={() => reload('swapSettings')}>
      {(settings) => <SwapForm settings={settings} />}
    </Loaded>
  );
}

function SwapForm({ settings }: { settings: SwapSettings }) {
  const { saveSwapSettings } = useDevice();
  const form = useForm({ sizeGb: String(settings.sizeGb) }, (values) => saveSwapSettings(Number(values.sizeGb)));
  const { values, setField, fieldErrors } = form;
  const sizes = Array.from({ length: Math.max(settings.maxGb, settings.sizeGb) + 1 }, (_, size) => ({
    value: String(size),
    label: size === 0 ? 'Off' : `${size} GB`,
  }));

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  return (
    <form onSubmit={handleSubmit} noValidate>
      <Section
        title="Swap"
        footer={`Room on the SD card for memory the Pi isn't using, so a program that briefly needs more memory than the Pi has keeps running. Up to ${settings.maxGb} GB fits: 1 GB of the card stays free. RasBeacon only swaps when memory is really short, because it wears the card.`}
      >
        <SelectField
          label="Swap file size"
          options={sizes}
          value={values.sizeGb}
          onChange={(sizeGb) => setField('sizeGb', sizeGb)}
          error={fieldErrors.sizeGb}
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
