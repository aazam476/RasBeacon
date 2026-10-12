import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import { AlertDialog, Button, ButtonRow, Disclosure, PasswordField, Section } from '@/ui';

/** Restarts RasBeacon after asking. */
export function RebootSection() {
  const { reboot } = useDevice();
  const [confirming, setConfirming] = useState(false);

  return (
    <Section footer="Connected devices disconnect while RasBeacon restarts, for about a minute.">
      <ButtonRow
        tone="danger"
        leading={{ icon: 'restart_alt', className: 'text-red' }}
        onClick={() => setConfirming(true)}
      >
        Reboot
      </ButtonRow>
      {confirming && (
        <AlertDialog
          title="Reboot RasBeacon?"
          tone="danger"
          confirmLabel="Reboot"
          busyLabel="Rebooting…"
          onConfirm={reboot}
          onClose={() => setConfirming(false)}
        >
          <p>All connected devices disconnect while it restarts.</p>
        </AlertDialog>
      )}
    </Section>
  );
}

/** Erases everything and restarts into first-time setup. Tucked away, red, and confirmed twice. */
export function FactoryResetSection() {
  const { factoryReset } = useDevice();
  const [password, setPassword] = useState('');
  const [confirming, setConfirming] = useState(false);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (password !== '') setConfirming(true);
  }

  return (
    <Section>
      <Disclosure
        tone="danger"
        leading={{ icon: 'delete_forever', className: 'text-red' }}
        title="Factory reset"
        content="rows"
      >
        <form onSubmit={handleSubmit} noValidate className="row-group">
          <p className="row text-subhead text-label-2">
            Erases every setting, including the admin password, and restarts into first-time setup.
          </p>
          <PasswordField
            label="Admin password"
            autoComplete="current-password"
            value={password}
            onChange={setPassword}
          />
          <div className="row">
            <Button type="submit" variant="danger" icon="delete_forever" disabled={password === ''}>
              Erase everything
            </Button>
          </div>
        </form>
      </Disclosure>
      {confirming && (
        <AlertDialog
          title="Erase everything?"
          tone="danger"
          confirmLabel="Erase everything"
          busyLabel="Erasing…"
          onConfirm={() => factoryReset(password)}
          onClose={() => setConfirming(false)}
        >
          <p>Every setting goes, and RasBeacon restarts into first-time setup. This can&apos;t be undone.</p>
        </AlertDialog>
      )}
    </Section>
  );
}
