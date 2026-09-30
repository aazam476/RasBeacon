// Logic for the system page: diagnostics download, reboot, factory reset
// and logout.

import { useState } from "react";
import { diagnosticsUrl, factoryReset, logout, reboot } from "../../api/client";

export interface SystemLogic {
  diagnosticsUrl: string;
  /** A short message after an action, e.g. "Restarting…". */
  message: string | null;
  error: string | null;
  reboot: () => void;
  resetPassword: string;
  setResetPassword: (value: string) => void;
  factoryReset: () => void;
  logout: () => void;
}

/** onLoggedOut is called after logout or a factory reset. */
export function useSystem(onLoggedOut: () => void): SystemLogic {
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [resetPassword, setResetPassword] = useState("");

  async function run(action: () => Promise<void>, success: string) {
    setError(null);
    try {
      await action();
      setMessage(success);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return {
    diagnosticsUrl,
    message,
    error,
    reboot: () => {
      if (
        window.confirm("Restart RasBeacon? Connected devices will be offline for about a minute.")
      ) {
        void run(reboot, "Restarting. Reconnect to the hotspot in about a minute.");
      }
    },
    resetPassword,
    setResetPassword,
    factoryReset: () => {
      if (
        window.confirm(
          "Erase ALL settings and restart into first-time setup? This can't be undone.",
        )
      ) {
        void run(async () => {
          await factoryReset(resetPassword);
          onLoggedOut();
        }, "All settings erased. RasBeacon is restarting into setup mode.");
      }
    },
    logout: () =>
      void run(async () => {
        await logout();
        onLoggedOut();
      }, "Logged out."),
  };
}
