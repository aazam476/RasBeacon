// Markup for the HTTPS page.

import type { HTTPSSettingsLogic } from "./useHTTPSSettings";

export type HTTPSSettingsViewProps = HTTPSSettingsLogic;

export function HTTPSSettingsView(props: HTTPSSettingsViewProps) {
  const { settings } = props;
  if (!settings) {
    return (
      <section>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</section>
    );
  }

  return (
    <section>
      <h2>HTTPS</h2>
      <p>
        HTTPS encrypts the connection between your browser and these settings pages. RasBeacon makes
        its own certificate, so browsers show a warning the first time: check that the fingerprint
        your browser shows matches the one below, then continue.
      </p>
      {props.message && <p>{props.message}</p>}
      {props.error && <p role="alert">{props.error}</p>}

      <label>
        <input
          type="checkbox"
          checked={settings.enabled}
          disabled={props.busy}
          onChange={(e) => props.setEnabled(e.target.checked)}
        />
        Use HTTPS for these settings pages
      </label>
      {settings.enabled && !props.onHTTPS && (
        <p>
          You are on plain HTTP. <a href={props.httpsAddress}>Continue at {props.httpsAddress}</a>
        </p>
      )}

      <h3>Certificate</h3>
      {settings.fingerprint ? (
        <dl>
          <dt>Name</dt>
          <dd>{settings.subject}</dd>
          <dt>Made by</dt>
          <dd>{settings.uploaded ? "You (uploaded)" : "RasBeacon"}</dd>
          <dt>Valid until</dt>
          <dd>{settings.validUntil}</dd>
          <dt>SHA-256 fingerprint</dt>
          <dd>
            <code>{settings.fingerprint}</code>
          </dd>
        </dl>
      ) : (
        <p>None yet: one is made when you turn HTTPS on.</p>
      )}
      <button type="button" onClick={props.generateNew} disabled={props.busy}>
        Make a new certificate
      </button>

      <h3>Upload your own certificate</h3>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.upload();
        }}
      >
        <label>
          Certificate file (PEM)
          <input
            type="file"
            accept=".pem,.crt"
            onChange={(e) => props.setFile("certificate", e.target.files?.[0] ?? null)}
          />
        </label>
        <label>
          Private key file (PEM, without a password)
          <input
            type="file"
            accept=".pem,.key"
            onChange={(e) => props.setFile("key", e.target.files?.[0] ?? null)}
          />
        </label>
        <button
          type="submit"
          disabled={props.busy || props.certificateText === "" || props.keyText === ""}
        >
          Upload
        </button>
      </form>
    </section>
  );
}
