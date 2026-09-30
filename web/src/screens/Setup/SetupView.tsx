// Markup for the first-time setup wizard. Receives everything from
// useSetup() through props; contains no API calls and no rules.

import type { SetupLogic } from "./useSetup";
import { FieldError } from "../../shared/FieldError";

export type SetupViewProps = SetupLogic;

export function SetupView(props: SetupViewProps) {
  const { values, setField, errors } = props;

  if (props.result) {
    return (
      <main>
        <h1>Setup complete</h1>
        <p>
          In a few seconds, the setup network will turn off and your hotspot{" "}
          <strong>{props.result.hotspotSsid}</strong> will start.
        </p>
        <ol>
          <li>
            Open your Wi-Fi settings and join {props.result.hotspotSsid} with the password you
            chose.
          </li>
          <li>
            Come back to <a href={props.result.adminUrl}>{props.result.adminUrl}</a> to see the
            status page.
          </li>
        </ol>
      </main>
    );
  }

  return (
    <main>
      <h1>Set up RasBeacon</h1>
      {!props.setupNetworkSecured && (
        <p>
          This setup network has no password. Finish setup now: the first person to choose an admin
          password owns the device.
        </p>
      )}
      {props.minutesLeft !== null && (
        <p>The setup network closes in about {props.minutesLeft} minutes.</p>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.submit();
        }}
      >
        <fieldset>
          <legend>Admin password</legend>
          <p>You'll use this to open these settings later. At least 10 characters.</p>
          <label>
            Admin password
            <input
              type="password"
              autoComplete="new-password"
              value={values.adminPassword}
              onChange={(e) => setField("adminPassword", e.target.value)}
            />
          </label>
          <FieldError message={errors.adminPassword} />
          <label>
            Type it again
            <input
              type="password"
              autoComplete="new-password"
              value={values.adminPasswordConfirm}
              onChange={(e) => setField("adminPasswordConfirm", e.target.value)}
            />
          </label>
          <FieldError message={errors.adminPasswordConfirm} />
        </fieldset>

        <fieldset>
          <legend>Your hotspot</legend>
          <p>This is the Wi-Fi network your devices will join.</p>
          <label>
            Network name
            <input
              type="text"
              value={values.hotspotSsid}
              onChange={(e) => setField("hotspotSsid", e.target.value)}
            />
          </label>
          <FieldError message={errors.hotspotSsid} />
          <label>
            Wi-Fi password (8 to 63 characters)
            <input
              type="text"
              autoComplete="off"
              value={values.hotspotPassword}
              onChange={(e) => setField("hotspotPassword", e.target.value)}
            />
          </label>
          <FieldError message={errors.hotspotPassword} />
          <label>
            Band
            <select
              value={values.hotspotBand}
              onChange={(e) => setField("hotspotBand", e.target.value as typeof values.hotspotBand)}
            >
              <option value="2.4GHz">2.4 GHz (works with every device, longer range)</option>
              <option value="5GHz">5 GHz (faster, shorter range)</option>
            </select>
          </label>
        </fieldset>

        {props.formError && <p role="alert">{props.formError}</p>}
        <button type="submit" disabled={props.submitting}>
          {props.submitting ? "Saving…" : "Finish setup"}
        </button>
      </form>
    </main>
  );
}
