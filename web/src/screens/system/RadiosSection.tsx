import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Radio, RadioRole, Status } from '@/data/types';
import {
  assignRoles,
  radioName,
  ROLE_LABELS,
  rolesNotes,
  rolesProblem,
  savedChoices,
  timeLeft,
  type RadioChoice,
} from '@/lib/radios';
import { ActionButton, Badge, ButtonRow, Notice, NoticeList, Row, Section, SelectField, Sheet, type Tone } from '@/ui';

const TONES: Record<RadioRole, Tone> = { hotspot: 'green', uplink: 'tint', guest: 'orange', unused: 'gray' };

/** What a radio is, after its name: its driver and how many networks it can broadcast. */
function radioDetails(radio: Radio): string {
  // The newest standard it can run a hotspot with (Wi-Fi 7 isn't used yet, but worth knowing).
  const best = radio.hasWifi7 ? 'Wi-Fi 7' : radio.has6GHz ? 'Wi-Fi 6E' : radio.hasWifi6 ? 'Wi-Fi 6' : '';
  return [radio.driver, radio.interface, best, radio.maxAps > 1 && `can broadcast ${radio.maxAps} networks`]
    .filter(Boolean)
    .join(' · ');
}

/** The Wi-Fi radios RasBeacon found, what each one does, and the way to change it. */
export function RadiosSection({ status }: { status: Status }) {
  const [changing, setChanging] = useState(false);
  return (
    <Section
      title="Wi-Fi radios"
      footer="RasBeacon picks by itself: the first USB adapter runs the hotspot and the built-in Wi-Fi joins the internet. With a second adapter, the guest network can get a radio of its own."
    >
      {status.radios.map((radio) => (
        <Row
          key={radio.mac || radio.interface}
          leading={{ icon: 'wifi' }}
          title={radioName(radio)}
          subtitle={radioDetails(radio)}
          trailing={<Badge tone={TONES[radio.role]}>{ROLE_LABELS[radio.role]}</Badge>}
        />
      ))}
      {status.radios.length > 1 && (
        <ButtonRow onClick={() => setChanging(true)} disabled={Boolean(status.radioRolesPending)}>
          Change what each radio does
        </ButtonRow>
      )}
      {changing && <RadioRolesSheet radios={status.radios} onClose={() => setChanging(false)} />}
    </Section>
  );
}

/** A choice per radio, what "Automatic" picks now, and what to know before RasBeacon restarts. */
function RadioRolesSheet({ radios, onClose }: { radios: Radio[]; onClose: () => void }) {
  const { saveRadioRoles } = useDevice();
  const [choices, setChoices] = useState(() => savedChoices(radios));
  const roles = assignRoles(radios, choices);
  const problem = rolesProblem(radios, choices);
  const unchanged = radios.every((radio) => choices[radio.mac] === savedChoices(radios)[radio.mac]);

  async function save() {
    const chosen = Object.fromEntries(
      Object.entries(choices).filter((entry): entry is [string, RadioRole] => entry[1] !== 'auto'),
    );
    await saveRadioRoles(chosen);
  }

  return (
    <Sheet
      title="Wi-Fi radios"
      confirmLabel="Save and restart"
      busyLabel="Restarting…"
      confirmDisabled={unchanged || problem !== undefined}
      onConfirm={save}
      onClose={onClose}
    >
      {(errors) => (
        <>
          <Section
            level={3}
            footer="RasBeacon restarts, and devices on the hotspot disconnect for about a minute. If you can’t reach RasBeacon afterwards, it goes back to the radios’ jobs from before after 3 minutes."
          >
            {radios.map((radio) => {
              const options: { value: RadioChoice; label: string }[] = [
                { value: 'auto', label: 'Automatic' },
                ...(radio.supportsAp
                  ? [
                      { value: 'hotspot' as const, label: ROLE_LABELS.hotspot },
                      { value: 'guest' as const, label: ROLE_LABELS.guest },
                    ]
                  : []),
                { value: 'uplink', label: ROLE_LABELS.uplink },
                { value: 'unused', label: ROLE_LABELS.unused },
              ];
              return (
                <SelectField
                  key={radio.mac}
                  label={radioName(radio)}
                  hint={
                    choices[radio.mac] === 'auto'
                      ? `Automatic: ${ROLE_LABELS[roles[radio.mac]]}. ${radioDetails(radio)}`
                      : radioDetails(radio)
                  }
                  options={options}
                  value={choices[radio.mac]}
                  onChange={(choice) => setChoices((current) => ({ ...current, [radio.mac]: choice }))}
                />
              );
            })}
          </Section>
          {(problem ?? errors.roles) && (
            <Notice tone="danger">
              <p>{problem ?? errors.roles}</p>
            </Notice>
          )}
          {!problem && rolesNotes(radios, roles).length > 0 && (
            <NoticeList tone="warning" ariaLabel="Before you save" messages={rolesNotes(radios, roles)} />
          )}
        </>
      )}
    </Sheet>
  );
}

/**
 * After the radios' jobs changed: keep them, or go back (RasBeacon goes back by itself when the
 * time runs out, in case the owner can't reach it any more).
 */
export function RadioRolesNotice({ secondsLeft }: { secondsLeft: number | undefined }) {
  const { keepRadioRoles, revertRadioRoles } = useDevice();
  if (!secondsLeft) return null;
  return (
    <Notice
      tone="warning"
      title="Keep the radios’ new jobs?"
      actions={
        <>
          <ActionButton size="sm" action={keepRadioRoles}>
            Keep
          </ActionButton>
          <ActionButton size="sm" variant="secondary" action={revertRadioRoles} busyLabel="Going back…">
            Go back
          </ActionButton>
        </>
      }
    >
      <p>RasBeacon goes back to the jobs from before in {timeLeft(secondsLeft)} unless you keep them.</p>
    </Notice>
  );
}
