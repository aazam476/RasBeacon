import { useState, type FormEvent } from 'react';
import { useDevice } from '@/data/device';
import { useAction } from '@/lib/useAction';
import { FullPage, FullPageHeader } from '@/shell/FullPage';
import { Button, FormError, FormSwitchRow, PasswordField, Section } from '@/ui';

/** Administrator login. The page underneath opens afterwards. */
export function LoginScreen() {
  const { logIn } = useDevice();
  const [password, setPassword] = useState('');
  const [stayLoggedIn, setStayLoggedIn] = useState(true);
  const loggingIn = useAction(() => logIn(password, stayLoggedIn));

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    loggingIn.run();
  }

  return (
    <FullPage title="Log in" centered>
      <FullPageHeader title="Log in">
        <p>Enter the admin password to manage RasBeacon.</p>
      </FullPageHeader>

      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-5">
        <Section>
          <PasswordField
            label="Admin password"
            autoComplete="current-password"
            enterKeyHint="go"
            autoFocus
            value={password}
            onChange={setPassword}
          />
          <FormSwitchRow label="Stay logged in" checked={stayLoggedIn} onChange={setStayLoggedIn} />
        </Section>
        {loggingIn.error && <FormError className="px-4">{loggingIn.error}</FormError>}
        <Button type="submit" size="lg" loading={loggingIn.pending}>
          {loggingIn.pending ? 'Logging in…' : 'Log in'}
        </Button>
      </form>

      <p className="px-4 text-footnote text-label-2">
        <strong className="font-semibold text-label">Forgot the password?</strong> Do a factory reset: put the SD card
        in a computer, create an empty file named <code className="font-mono text-label">factory-reset</code> on it, and
        start RasBeacon again.
      </p>
    </FullPage>
  );
}
