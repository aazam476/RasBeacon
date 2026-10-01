// Markup for the captive portal notice (#portal).

import type { PortalLogic } from "./usePortal";
import { formatDuration } from "../../shared/format";
import { PortalBrowserView } from "./PortalBrowserView";

export type PortalViewProps = PortalLogic;

export function PortalView(props: PortalViewProps) {
  const { status } = props;
  if (!status) {
    return <main>{props.loadError ? <p role="alert">{props.loadError}</p> : <p>Loading…</p>}</main>;
  }

  return (
    <main>
      <h1>This network needs a login</h1>
      {props.actionError && <p role="alert">{props.actionError}</p>}

      {status.browsing ? (
        <PortalBrowserView {...props.browser} />
      ) : status.loginMode ? (
        <>
          <p>
            <strong>Login mode is on</strong> for {formatDuration(status.secondsLeft)} more.
          </p>
          <p>
            <a href={props.loginUrl}>Open the login page</a> and log in (or accept the terms).
            RasBeacon notices within a few seconds and turns the VPN back on.
          </p>
          <button type="button" onClick={props.extend} disabled={props.busy}>
            10 more minutes
          </button>
          <button type="button" onClick={props.stop} disabled={props.busy}>
            Stop login mode
          </button>
          <p>
            While login mode is on, web and DNS traffic from all devices on the hotspot goes
            directly to this network, not through the VPN.
          </p>
        </>
      ) : status.canStart || status.canBrowse ? (
        <>
          <p>
            The network RasBeacon is connected to (a hotel or café, for example) shows a login page
            before letting anyone online.
            {status.canStart &&
              " Your VPN's kill switch keeps your devices offline until then, so nobody can reach that page."}
          </p>
          {status.canBrowse && (
            <>
              <h2>Log in through RasBeacon (recommended)</h2>
              <p>
                RasBeacon opens the login page itself and shows it here. Nothing from your devices
                leaves the VPN.
              </p>
              <button type="button" onClick={props.browser.open} disabled={props.browser.busy}>
                {props.browser.busy ? "Opening…" : "Open the login page here"}
              </button>
              {props.browser.error && <p role="alert">{props.browser.error}</p>}
            </>
          )}
          {status.canStart && (
            <>
              <h2>Login mode</h2>
              <p>
                Lets your devices reach the login page for 10 minutes. During that time, web and DNS
                traffic from all devices on the hotspot goes directly to this network,{" "}
                <strong>not through the VPN</strong>. It ends by itself as soon as the login works.
              </p>
              <button type="button" onClick={props.start} disabled={props.busy}>
                Start login mode
              </button>
            </>
          )}
          <p>
            <button type="button" onClick={props.check} disabled={props.busy}>
              Check again
            </button>{" "}
            (if you already logged in another way)
          </p>
        </>
      ) : (
        <p>
          Nothing to do:{" "}
          {status.detected
            ? "your devices can reach the login page directly."
            : "RasBeacon doesn't see a login page right now."}{" "}
          <a href="/">Open RasBeacon</a>{" "}
          <button type="button" onClick={props.check} disabled={props.busy}>
            Check again
          </button>
        </p>
      )}
    </main>
  );
}
