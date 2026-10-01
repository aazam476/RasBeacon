// Markup for the system page.

import type { SystemLogic } from "./useSystem";

export type SystemViewProps = SystemLogic;

export function SystemView(props: SystemViewProps) {
  return (
    <section>
      <h2>System</h2>
      {props.message && <p>{props.message}</p>}
      {props.error && <p role="alert">{props.error}</p>}

      <h3>Software</h3>
      <p>
        Version {props.version || "…"}
        {props.update?.board &&
          ` for ${props.update.board === "pi5" ? "Raspberry Pi 5" : "Raspberry Pi 4"}`}
        .
      </p>
      {props.update?.message && <p>{props.update.message}</p>}
      {props.update?.latestVersion && (
        <p>
          A new version is available:{" "}
          <a href={props.update.latestUrl} target="_blank" rel="noreferrer">
            {props.update.latestVersion}
          </a>{" "}
          (download its .rbupdate file for your board, then install it below).
        </p>
      )}
      {props.update?.canCheckOnline && (
        <label>
          <input
            type="checkbox"
            checked={props.update.checkOnline}
            onChange={(e) => props.setCheckOnline(e.target.checked)}
          />
          Check online for new versions (asks GitHub every few hours; never installs anything by
          itself)
        </label>
      )}
      {props.update && !props.update.canInstall ? (
        <p>
          This image can&apos;t install update files (it has no update signing key). Flash new
          versions instead.
        </p>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            props.installUpdate();
          }}
        >
          <label>
            Update file (.rbupdate, from the Releases page)
            <input
              type="file"
              accept=".rbupdate"
              onChange={(e) => props.setUpdateFile(e.target.files?.[0] ?? null)}
            />
          </label>
          <button type="submit" disabled={!props.updateFile || props.installing}>
            {props.installing ? "Installing…" : "Install update"}
          </button>
        </form>
      )}

      <h3>Diagnostics</h3>
      <p>
        A text report to attach when asking for help. Passwords are removed and device addresses are
        partly hidden.
      </p>
      <a href={props.diagnosticsUrl} download>
        Download diagnostics
      </a>

      <h3>Restart</h3>
      <button type="button" onClick={props.reboot}>
        Restart RasBeacon
      </button>

      <h3>Factory reset</h3>
      <p>Erases every setting, including the admin password, and restarts into first-time setup.</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.factoryReset();
        }}
      >
        <label>
          Admin password
          <input
            type="password"
            autoComplete="current-password"
            value={props.resetPassword}
            onChange={(e) => props.setResetPassword(e.target.value)}
          />
        </label>
        <button type="submit" disabled={props.resetPassword === ""}>
          Erase everything
        </button>
      </form>

      <h3>Log out</h3>
      <button type="button" onClick={props.logout}>
        Log out
      </button>
    </section>
  );
}
