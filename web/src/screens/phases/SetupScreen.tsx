import { useState, type ReactNode } from 'react';
import { useDevice } from '@/data/device';
import type { ClaimResponse, SetupForm, SetupPlace, SetupState } from '@/data/types';
import { useCountdown } from '@/lib/useCountdown';
import { useForm } from '@/lib/useForm';
import { FullPage, FullPageHeader } from '@/shell/FullPage';
import {
  Button,
  Choice,
  ChoiceList,
  FormError,
  Icon,
  Notice,
  PasswordField,
  Section,
  SelectField,
  TextField,
} from '@/ui';
import { bandOptions } from '../common/wifiOptions';

/**
 * First-time setup: the admin password and RasBeacon's own Wi-Fi network. Afterwards it says
 * which network to join, because the setup network turns off.
 */
export function SetupScreen() {
  const { setup } = useDevice();
  const [result, setResult] = useState<{ answer: ClaimResponse; place: SetupPlace } | null>(null);

  if (result?.answer) return <SetupComplete result={result.answer} place={result.place} />;
  if (!setup) return null;
  return <SetupWizard setup={setup} onDone={(answer, place) => setResult({ answer, place })} />;
}

/** Typing settings for the network name: no capitals, corrections or suggestions. */
const PLAIN_INPUT = { autoComplete: 'off', autoCapitalize: 'none', autoCorrect: 'off', spellCheck: false } as const;

function SetupWizard({
  setup,
  onDone,
}: {
  setup: SetupState;
  onDone: (result: ClaimResponse, place: SetupPlace) => void;
}) {
  const { finishSetup } = useDevice();
  const form = useForm<SetupForm>(
    {
      adminPassword: '',
      adminPasswordConfirm: '',
      hotspotSsid: setup.suggestedHotspotSsid ?? '',
      hotspotPassword: setup.suggestedHotspotPassword ?? '',
      hotspotBand: '2.4GHz',
      place: 'travel',
    },
    async (values) => onDone(await finishSetup(values), values.place),
  );
  const { values, setField, fieldErrors } = form;

  return (
    <FullPage title="Set up RasBeacon" width="lg">
      <FullPageHeader title="Set up RasBeacon">
        <p>Choose an admin password and your Wi-Fi network. It takes about a minute.</p>
      </FullPageHeader>

      {!setup.setupNetworkSecured && (
        <Notice tone="warning" role="alert" title="This setup network has no password">
          <p>Finish setup now: the first person to choose an admin password owns the device.</p>
        </Notice>
      )}
      {setup.setupClosesInSeconds !== undefined && <ClosingNotice seconds={setup.setupClosesInSeconds} />}

      <form
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          form.submit();
        }}
        className="flex flex-col gap-7"
      >
        <Section title="Admin password" footer="You'll use this to open these settings later. At least 10 characters.">
          <PasswordField
            label="Admin password"
            autoComplete="new-password"
            value={values.adminPassword}
            onChange={(value) => setField('adminPassword', value)}
            error={fieldErrors.adminPassword}
          />
          <PasswordField
            label="Type it again"
            autoComplete="new-password"
            value={values.adminPasswordConfirm}
            onChange={(value) => setField('adminPasswordConfirm', value)}
            error={fieldErrors.adminPasswordConfirm}
          />
        </Section>

        <Section title="Your hotspot" footer="This is the Wi-Fi network your devices will join.">
          <TextField
            label="Network name"
            value={values.hotspotSsid}
            onChange={(value) => setField('hotspotSsid', value)}
            error={fieldErrors.hotspotSsid}
            {...PLAIN_INPUT}
          />
          <PasswordField
            label="Wi-Fi password"
            labelNote="(8 to 63 characters)"
            initiallyVisible
            value={values.hotspotPassword}
            onChange={(value) => setField('hotspotPassword', value)}
            error={fieldErrors.hotspotPassword}
            {...PLAIN_INPUT}
          />
          <SelectField
            label="Band"
            options={bandOptions(setup.no5GHz)}
            value={values.hotspotBand}
            onChange={(value) => setField('hotspotBand', value)}
            error={fieldErrors.hotspotBand}
          />
        </Section>

        <Section
          title="Where will you use RasBeacon?"
          footer="This only changes the next steps shown after setup. You can use RasBeacon anywhere, either way."
        >
          <ChoiceList
            ariaLabel="Where will you use RasBeacon?"
            value={values.place}
            onChange={(place) => setField('place', place)}
          >
            <Choice
              value="travel"
              title="Traveling"
              description="It joins the Wi-Fi of a hotel, a café or a campus, or a cable in the wall."
              icon="public"
            />
            <Choice
              value="home"
              title="At home"
              description="A cable connects its Ethernet port to your home router."
              icon="settings_ethernet"
            />
          </ChoiceList>
        </Section>

        {form.error && <FormError className="px-4">{form.error}</FormError>}
        <Button type="submit" size="lg" loading={form.pending}>
          {form.pending ? 'Saving…' : 'Finish setup'}
        </Button>
      </form>
    </FullPage>
  );
}

/** "The setup network closes in about 27 minutes", counting down. */
function ClosingNotice({ seconds }: { seconds: number }) {
  const minutes = Math.ceil(useCountdown(seconds) / 60);

  return (
    <Notice tone="neutral" icon="timer">
      <p>
        {minutes > 0
          ? `The setup network closes in about ${minutes} ${minutes === 1 ? 'minute' : 'minutes'}.`
          : 'The setup network is closing now.'}
      </p>
    </Notice>
  );
}

function Step({ number, children }: { number: number; children: ReactNode }) {
  return (
    <li className="row items-start">
      <span
        aria-hidden="true"
        className="row-tile grid size-[1.8125rem] shrink-0 place-items-center rounded-full bg-tint/12 text-subhead font-semibold text-tint lg:size-6"
      >
        {number}
      </span>
      <span className="min-w-0 flex-1 pt-0.5 text-body wrap-break-word text-label lg:pt-0">{children}</span>
    </li>
  );
}

function SetupComplete({ result, place }: { result: ClaimResponse; place: SetupPlace }) {
  const secondsLeft = useCountdown(result.switchInSeconds);
  const adminLink = (
    <a href={result.adminUrl} className="selectable font-mono text-tint underline">
      {result.adminUrl}
    </a>
  );

  return (
    <FullPage title="Setup complete" centered>
      <FullPageHeader
        title="Setup complete"
        icon={
          <span
            aria-hidden="true"
            className="grid size-[4.5rem] place-items-center rounded-full bg-green-fill/15 text-green"
          >
            <Icon name="check_circle" filled size={44} />
          </span>
        }
      >
        <p>
          Join <strong className="font-semibold wrap-anywhere text-label">{result.hotspotSsid}</strong>, then open{' '}
          {adminLink}
        </p>
      </FullPageHeader>

      <p role="status" className="text-center text-subhead text-label-2">
        {secondsLeft > 0
          ? `The setup network turns off in ${secondsLeft} ${secondsLeft === 1 ? 'second' : 'seconds'}, and your hotspot starts.`
          : 'The setup network is off now, and your hotspot is starting.'}
      </p>

      <Section title="Next">
        <ol>
          <Step number={1}>
            Open your Wi-Fi settings and join <strong className="font-semibold">{result.hotspotSsid}</strong> with the
            password you chose.
          </Step>
          <Step number={2}>Come back to {adminLink} to see the status page.</Step>
          {place === 'home' ? (
            <Step number={3}>
              Connect RasBeacon’s Ethernet port to your home router with a cable. It uses the internet from there by
              itself.
            </Step>
          ) : (
            <Step number={3}>
              On the Internet page, join the Wi-Fi where you are, or plug in a cable from the wall. If the network has a
              login page, RasBeacon helps you through it.
            </Step>
          )}
        </ol>
      </Section>
    </FullPage>
  );
}
