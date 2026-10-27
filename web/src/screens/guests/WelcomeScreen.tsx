import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import type { WelcomeNetworks, WelcomeSettings } from '@/data/types';
import { useForm } from '@/lib/useForm';
import { ASK_AGAIN_OPTIONS, MAX_LOGO, MAX_PAGE } from '@/lib/welcome';
import { Loaded } from '@/shell/Loaded';
import { usePageTitle } from '@/shell/navigation';
import {
  ButtonRow,
  Disclosure,
  FieldRow,
  FileField,
  FormActions,
  FormSwitchRow,
  Notice,
  Row,
  Section,
  SelectField,
  TextAreaField,
  TextField,
} from '@/ui';

const NETWORK_OPTIONS: readonly { value: WelcomeNetworks; label: string }[] = [
  { value: 'guest', label: 'Guest network' },
  { value: 'hotspot', label: 'Your hotspot' },
  { value: 'both', label: 'Both' },
];

/**
 * Guests → Welcome page: your own captive portal. Devices see it before they get the internet,
 * with your message, house rules and an access code if you like.
 */
export function WelcomeScreen() {
  usePageTitle('Welcome page');
  const { welcomeSettings, reload } = useDevice();
  return (
    <Loaded data={welcomeSettings} what="the welcome page" onRetry={() => reload('welcomeSettings')}>
      {(settings) => <WelcomeForm settings={settings} />}
    </Loaded>
  );
}

/** Reads a chosen file as a data: URL (for the logo). */
function readDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error('The file could not be read.'));
    reader.readAsDataURL(file);
  });
}

function WelcomeForm({ settings }: { settings: WelcomeSettings }) {
  const { saveWelcomeSettings, guestSettings } = useDevice();
  const form = useForm(settings, saveWelcomeSettings);
  const { values, setField, fieldErrors } = form;
  // Problems with a chosen file, found before saving (too big, unreadable).
  const [fileError, setFileError] = useState<{ logo?: string; customPage?: string }>({});
  // A file input can't be emptied from code; a new key makes a new one.
  const [fileKey, setFileKey] = useState(0);
  const guestOff = values.networks !== 'hotspot' && guestSettings.data?.enabled === false;

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    form.submit();
  }

  async function chooseLogo(files: File[]) {
    setFileError((errors) => ({ ...errors, logo: undefined }));
    if (!files[0]) return;
    const url = await readDataUrl(files[0]).catch(() => '');
    if (!/^data:image\/(png|jpeg);base64,/.test(url) || url.length > MAX_LOGO) {
      setFileError((errors) => ({ ...errors, logo: 'Use a PNG or JPEG picture of up to 100 KB.' }));
      return;
    }
    setField('logo', url);
  }

  async function choosePage(files: File[]) {
    setFileError((errors) => ({ ...errors, customPage: undefined }));
    if (!files[0]) return;
    const text = await files[0].text().catch(() => '');
    if (text === '' || new Blob([text]).size > MAX_PAGE) {
      setFileError((errors) => ({ ...errors, customPage: 'Use an HTML page of up to 64 KB.' }));
      return;
    }
    setField('customPage', text);
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-7">
      <Section footer="Until a device accepts the page, it is all the device sees: every website shows the page, and nothing else reaches the internet. These settings stay reachable from your hotspot.">
        <FormSwitchRow
          leading={{ tile: 'article', color: 'teal' }}
          label="Show a welcome page"
          checked={values.enabled}
          onChange={(enabled) => setField('enabled', enabled)}
        />
        <SelectField
          label="Show it on"
          options={NETWORK_OPTIONS}
          value={values.networks}
          onChange={(networks) => setField('networks', networks)}
          hint={guestOff ? 'The guest network is off, so only its part waits for it to be turned on.' : undefined}
          error={fieldErrors.networks}
        />
      </Section>

      <Section title="Page">
        <TextField
          label="Title"
          value={values.title}
          onChange={(title) => setField('title', title)}
          maxLength={60}
          error={fieldErrors.title}
        />
        <TextAreaField
          label="Message"
          labelNote="(optional)"
          rows={3}
          value={values.message}
          onChange={(message) => setField('message', message)}
          error={fieldErrors.message}
        />
        <TextAreaField
          label="House rules or terms"
          labelNote="(optional)"
          rows={5}
          hint="With these, devices must tick “I agree” first."
          value={values.terms}
          onChange={(terms) => setField('terms', terms)}
          error={fieldErrors.terms}
        />
      </Section>

      <Section title="Access">
        <TextField
          label="Access code"
          labelNote="(optional)"
          hint="Guests type it to get in, for example from a card in the room. After 5 wrong codes a device waits a minute."
          value={values.accessCode}
          onChange={(accessCode) => setField('accessCode', accessCode)}
          maxLength={32}
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          error={fieldErrors.accessCode}
        />
        <SelectField
          label="Ask again"
          options={ASK_AGAIN_OPTIONS}
          value={String(values.askAgainHours)}
          onChange={(hours) => setField('askAgainHours', Number(hours))}
          hint="“Only once” lasts until you ask a device again on the Clients page."
          error={fieldErrors.askAgainHours}
        />
        {values.networks !== 'guest' && (
          <FormSwitchRow
            label="Also for wired devices"
            description="Otherwise devices plugged in by cable get in without it."
            checked={values.includeWired}
            onChange={(includeWired) => setField('includeWired', includeWired)}
          />
        )}
      </Section>

      <Section title="Look" footer="Phones and laptops open the page by themselves when they join.">
        <FieldRow label="Color" error={fieldErrors.color}>
          {(control) => (
            <input
              type="color"
              value={values.color || '#0c8a9c'}
              onChange={(event) => setField('color', event.target.value)}
              className="h-9 w-16 cursor-pointer rounded-md border-0 bg-transparent p-0"
              {...control}
            />
          )}
        </FieldRow>
        {values.logo ? (
          <>
            <Row title="Logo" value={<img src={values.logo} alt="Logo" className="ml-auto max-h-12 max-w-32" />} />
            <ButtonRow tone="danger" onClick={() => setField('logo', '')}>
              Remove logo
            </ButtonRow>
          </>
        ) : (
          <FileField
            key={`logo-${fileKey}`}
            label="Logo"
            labelNote="(optional)"
            accept=".png,.jpg,.jpeg,image/png,image/jpeg"
            hint="A PNG or JPEG picture of up to 100 KB."
            onChange={(files) => void chooseLogo(files)}
            error={fileError.logo ?? fieldErrors.logo}
          />
        )}
      </Section>

      <Section>
        <Disclosure title="Your own page (advanced)" content="rows" defaultOpen={values.customPage !== ''}>
          {values.customPage ? (
            <>
              <Row
                title="Your page"
                value={`${Math.max(1, Math.round(new Blob([values.customPage]).size / 1024))} KB`}
              />
              <ButtonRow
                tone="danger"
                onClick={() => {
                  setField('customPage', '');
                  setFileKey((key) => key + 1);
                }}
              >
                Use the built-in page
              </ButtonRow>
            </>
          ) : (
            <FileField
              key={`page-${fileKey}`}
              label="HTML file"
              accept=".html,.htm,text/html"
              hint="Shown instead of the page above. Scripts don't run on it. Its form must send to /accept, with a field named code when you use an access code."
              onChange={(files) => void choosePage(files)}
              error={fileError.customPage ?? fieldErrors.customPage}
            />
          )}
        </Disclosure>
      </Section>

      {settings.enabled && (
        <Section title="Now">
          <Row title="Devices let in" value={settings.accepted} />
        </Section>
      )}
      {values.enabled && values.networks !== 'guest' && (
        <Notice tone="info">
          <p>Your own devices on the hotspot see the page too. Accept it on each of them once.</p>
        </Notice>
      )}

      <FormActions
        label="Save"
        busyLabel="Saving…"
        pending={form.pending}
        error={form.error}
        success={form.saved && 'Saved.'}
      />
    </form>
  );
}
