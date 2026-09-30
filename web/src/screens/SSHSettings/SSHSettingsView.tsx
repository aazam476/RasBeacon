// Markup for the SSH settings page.

import type { SSHSettingsLogic } from "./useSSHSettings";

export type SSHSettingsViewProps = SSHSettingsLogic;

export function SSHSettingsView(props: SSHSettingsViewProps) {
  const { values } = props;
  if (!values) {
    return (
      <section>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</section>
    );
  }

  return (
    <section>
      <h2>SSH (advanced)</h2>
      <p>
        SSH gives command-line access to RasBeacon. It is off by default. When on, it only accepts
        the public keys below (no passwords) and only from devices on the hotspot. Log in as{" "}
        <code>root</code>.
      </p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.save();
        }}
      >
        <label>
          <input
            type="checkbox"
            checked={values.enabled}
            onChange={(e) => props.setEnabled(e.target.checked)}
          />
          Turn on SSH
        </label>
        <label>
          Public keys (one per line, e.g. the contents of ~/.ssh/id_ed25519.pub)
          <textarea
            rows={5}
            value={values.authorizedKeys}
            onChange={(e) => props.setAuthorizedKeys(e.target.value)}
          />
        </label>
        {props.saveError && <p role="alert">{props.saveError}</p>}
        {props.saved && <p>Saved.</p>}
        <button type="submit" disabled={props.saving}>
          {props.saving ? "Saving…" : "Save"}
        </button>
      </form>
    </section>
  );
}
