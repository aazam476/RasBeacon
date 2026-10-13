import type { FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { SwapSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { Loaded } from '@/shell/Loaded';
import { Disclosure, FormActions, Section, SelectField } from '@/ui';

/** The swap file's size: room on the SD card for memory the Pi isn't using. */
export function SwapSection() {
  const { swapSettings, reload } = useDevice();

  return (
    <Loaded data={swapSettings} what="the swap settings" onRetry={() => reload('swapSettings')}>
      {(settings) => (
        <Section
          footer={`Lets programs keep running when memory runs out. Used only when memory is really short, because it wears the card. ${settings.maxGb > 0 ? `Up to ${formatGb(settings.maxGb)} fits` : 'The card is too full for one'} (1 GB always stays free).`}
        >
          <Disclosure
            title="Swap file"
            subtitle={settings.sizeGb > 0 ? formatGb(settings.sizeGb) : 'Off'}
            leading={{ icon: 'swap_horiz' }}
            content="rows"
          >
            <SwapForm settings={settings} />
          </Disclosure>
        </Section>
      )}
    </Loaded>
  );
}

function SwapForm({ settings }: { settings: SwapSettings }) {
  const { saveSwapSettings } = useDevice();
  const form = useForm({ sizeGb: String(settings.sizeGb) }, (values) => saveSwapSettings(Number(values.sizeGb)));
  const { values, setField, fieldErrors } = form;
  const sizes = swapSizes(settings).map((size) => ({
    value: String(size),
    label: size === 0 ? 'Off' : formatGb(size),
  }));

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  const making = Number(values.sizeGb) > 0;
  const noRoom = settings.maxGb === 0 && settings.sizeGb === 0;

  return (
    <form onSubmit={handleSubmit} noValidate className="row-group">
      <SelectField
        label="Size"
        options={sizes}
        value={values.sizeGb}
        onChange={(sizeGb) => setField('sizeGb', sizeGb)}
        error={fieldErrors.sizeGb}
        disabled={noRoom || form.pending}
      />
      {!noRoom && (
        <div className="row">
          <FormActions
            inRow
            className="w-full"
            label="Save"
            // A large file takes a minute or two to set aside: say what is happening.
            busyLabel={making ? 'Making the swap file…' : 'Turning swap off…'}
            pending={form.pending}
            error={form.error}
            success={form.saved && 'Saved.'}
          />
        </div>
      )}
    </form>
  );
}

/**
 * The sizes to offer: off, then doubling from 1 GB, the largest that fits, and the current size,
 * so a large card or SSD doesn't list every gigabyte.
 */
function swapSizes({ sizeGb, maxGb }: SwapSettings): number[] {
  const sizes = new Set([0, sizeGb]);
  for (let size = 1; size <= maxGb; size *= 2) sizes.add(size);
  if (maxGb > 0) sizes.add(maxGb);
  return [...sizes].sort((a, b) => a - b);
}

function formatGb(gb: number) {
  // A no-break space, as elsewhere, so the unit never wraps away from the number.
  return `${gb.toLocaleString('en-US')}\u00a0GB`;
}
