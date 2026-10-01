// Logic for the captive portal notice (#portal). This page usually opens
// by itself in a phone's "Log in to network" sheet when the network
// RasBeacon is on shows a login page and the VPN's kill switch keeps
// devices offline. No RasBeacon login is needed here.
//
// Two ways to log in to the network are offered:
//   - the zero-leak login (recommended): the login page opens in
//     RasBeacon's own browser, shown here as pictures (usePortalBrowser);
//   - login mode: devices may reach the login page directly for a while.

import { useState } from "react";
import { getPortalStatus, portalAction } from "../../api/client";
import type { PortalStatus } from "../../api/types";
import { useLoad } from "../../shared/useLoad";
import { usePortalBrowser, type PortalBrowserLogic } from "./usePortalBrowser";

/** Opened after starting login mode if the network's page isn't known. */
export const FALLBACK_LOGIN_URL = "http://neverssl.com/";

export interface PortalLogic {
  status: PortalStatus | undefined;
  loadError: string | null;
  /** Where "Open the login page" goes. */
  loginUrl: string;
  start: () => void;
  extend: () => void;
  stop: () => void;
  /** Checks for a login page again right away. */
  check: () => void;
  /** The zero-leak login (RasBeacon's own browser). */
  browser: PortalBrowserLogic;
  busy: boolean;
  actionError: string | null;
}

export function usePortal(): PortalLogic {
  // Refresh often: the countdown, and login mode ends by itself.
  const loaded = useLoad(getPortalStatus, 5);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const loginUrl = loaded.data?.loginUrl || FALLBACK_LOGIN_URL;

  const browser = usePortalBrowser(loaded.data?.browsing ?? false, loaded.reload);

  async function act(action: "start" | "extend" | "stop" | "check") {
    setBusy(true);
    setActionError(null);
    try {
      await portalAction(action);
      loaded.reload();
      if (action === "check") {
        // The device checks within a few seconds.
        setTimeout(loaded.reload, 6000);
      }
      if (action === "start") {
        // Give RasBeacon a moment to open the way, then go to the network's
        // login page in this same window (or sheet).
        setTimeout(() => window.location.assign(loginUrl), 2000);
      }
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return {
    status: loaded.data,
    loadError: loaded.error,
    loginUrl,
    start: () => void act("start"),
    extend: () => void act("extend"),
    stop: () => void act("stop"),
    check: () => void act("check"),
    browser,
    busy,
    actionError,
  };
}
