// Markup for the login screen.

import type { LoginLogic } from "./useLogin";

export type LoginViewProps = LoginLogic;

export function LoginView(props: LoginViewProps) {
  return (
    <main>
      <h1>RasBeacon</h1>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.submit();
        }}
      >
        <label>
          Admin password
          <input
            type="password"
            autoComplete="current-password"
            autoFocus
            value={props.password}
            onChange={(e) => props.setPassword(e.target.value)}
          />
        </label>
        {props.error && <p role="alert">{props.error}</p>}
        <button type="submit" disabled={props.submitting || props.password === ""}>
          {props.submitting ? "Checking…" : "Log in"}
        </button>
      </form>
      <p>
        Forgot the password? Do a factory reset: put the SD card in a computer, create an empty file
        named <code>factory-reset</code> on it, and start RasBeacon again.
      </p>
    </main>
  );
}
