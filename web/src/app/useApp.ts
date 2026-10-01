// Decides which screen to show: setup wizard, login, or the main pages.
//
// On load it asks the device two questions:
//   1. What mode are you in? (GET /api/v1/setup, no login needed)
//   2. Is this browser logged in? (GET /api/v1/session)

import { useCallback, useEffect, useState } from "react";
import { getSession, getSetupState } from "../api/client";
import type { SetupState } from "../api/types";
import { useLoad } from "../shared/useLoad";

/** The pages available after login. Shown in the address bar as #status etc. */
export const PAGES = ["status", "internet", "vpn", "hotspot", "ssh", "https", "system"] as const;
export type Page = (typeof PAGES)[number];

export type AppPhase =
  | { kind: "loading" }
  | { kind: "unreachable"; error: string }
  | { kind: "notice"; mode: "setup-closed" | "setup-error" | "no-radio" }
  | { kind: "setup"; setupState: SetupState }
  | { kind: "login" }
  | { kind: "main" }
  /** The captive portal notice (#portal); works without logging in. */
  | { kind: "portal" };

export interface AppLogic {
  phase: AppPhase;
  page: Page;
  setPage: (page: Page) => void;
  onLoggedIn: () => void;
  onLoggedOut: () => void;
  retry: () => void;
}

/** Loads the setup state and session together. */
async function loadStartupInfo() {
  const [setupState, session] = await Promise.all([getSetupState(), getSession()]);
  return { setupState, loggedIn: session.loggedIn };
}

export function useApp(): AppLogic {
  const startup = useLoad(loadStartupInfo);
  // The part of the address after "#", e.g. "hotspot" or "portal". The page
  // is kept there so reloading stays on it.
  const [hash, setHash] = useState(currentHash());

  const setPage = useCallback((next: Page) => {
    window.location.hash = next;
    setHash(next);
  }, []);
  useEffect(() => {
    const onHashChange = () => setHash(currentHash());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  return {
    phase: decidePhase(startup.data, startup.error, hash === "portal"),
    page: pageFromHash(hash),
    setPage,
    onLoggedIn: startup.reload,
    onLoggedOut: () => {
      // The next person to log in starts on the status page.
      setPage("status");
      startup.reload();
    },
    retry: startup.reload,
  };
}

/**
 * Pure decision logic, exported for unit tests. wantsPortal is true when
 * the address is #portal (the notice page phones open by themselves).
 */
export function decidePhase(
  data: { setupState: SetupState; loggedIn: boolean } | undefined,
  error: string | null,
  wantsPortal = false,
): AppPhase {
  if (error && !data) return { kind: "unreachable", error };
  if (!data) return { kind: "loading" };

  const { setupState, loggedIn } = data;
  if (wantsPortal && setupState.mode === "normal") return { kind: "portal" };
  switch (setupState.mode) {
    case "setup":
      return { kind: "setup", setupState };
    case "setup-closed":
    case "setup-error":
    case "no-radio":
      // After setup these problems can't happen, but a logged-in owner
      // should still reach the settings, so only block unclaimed devices.
      if (!setupState.claimed) return { kind: "notice", mode: setupState.mode };
      break;
  }
  return loggedIn ? { kind: "main" } : { kind: "login" };
}

function currentHash(): string {
  return window.location.hash.replace("#", "");
}

function pageFromHash(hash: string): Page {
  return (PAGES as readonly string[]).includes(hash) ? (hash as Page) : "status";
}
