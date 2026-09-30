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
export const PAGES = ["status", "hotspot", "ssh", "system"] as const;
export type Page = (typeof PAGES)[number];

export type AppPhase =
  | { kind: "loading" }
  | { kind: "unreachable"; error: string }
  | { kind: "notice"; mode: "setup-closed" | "setup-error" | "no-radio" }
  | { kind: "setup"; setupState: SetupState }
  | { kind: "login" }
  | { kind: "main" };

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
  const [page, setPageState] = useState<Page>(pageFromHash());

  // Keep the page in the address bar (#hotspot) so reloading stays on it.
  const setPage = useCallback((next: Page) => {
    window.location.hash = next;
    setPageState(next);
  }, []);
  useEffect(() => {
    const onHashChange = () => setPageState(pageFromHash());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  return {
    phase: decidePhase(startup.data, startup.error),
    page,
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

/** Pure decision logic, exported for unit tests. */
export function decidePhase(
  data: { setupState: SetupState; loggedIn: boolean } | undefined,
  error: string | null,
): AppPhase {
  if (error && !data) return { kind: "unreachable", error };
  if (!data) return { kind: "loading" };

  const { setupState, loggedIn } = data;
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

function pageFromHash(): Page {
  const hash = window.location.hash.replace("#", "");
  return (PAGES as readonly string[]).includes(hash) ? (hash as Page) : "status";
}
