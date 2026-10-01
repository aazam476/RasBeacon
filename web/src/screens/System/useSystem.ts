// Logic for the system page: software version and updates, diagnostics
// download, reboot, factory reset and logout.

import { useState } from "react";
import {
  diagnosticsUrl,
  factoryReset,
  getStatus,
  installUpdate,
  logout,
  reboot,
  setCheckOnline,
} from "../../api/client";
import type { UpdateStatus } from "../../api/types";
import { useLoad } from "../../shared/useLoad";

export interface SystemLogic {
  /** RasBeacon's version, e.g. "v1.2.0". */
  version: string;
  update: UpdateStatus | undefined;
  updateFile: File | null;
  setUpdateFile: (file: File | null) => void;
  /** Asks for confirmation, then uploads and installs the update file. */
  installUpdate: () => void;
  installing: boolean;
  setCheckOnline: (on: boolean) => void;
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
  const status = useLoad(getStatus);
  const [updateFile, setUpdateFile] = useState<File | null>(null);
  const [installing, setInstalling] = useState(false);

  async function run(action: () => Promise<void>, success: string) {
    setError(null);
    try {
      await action();
      setMessage(success);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function install() {
    if (!updateFile) return;
    setInstalling(true);
    setMessage("Uploading and checking the update… keep this page open (a few minutes).");
    await run(async () => {
      await installUpdate(updateFile);
      setUpdateFile(null);
      status.reload();
    }, "Update installed. RasBeacon is restarting to try it; reconnect in about two minutes.");
    setInstalling(false);
  }

  return {
    version: status.data?.version ?? "",
    update: status.data?.update,
    updateFile,
    setUpdateFile,
    installUpdate: () => {
      if (
        window.confirm(
          "Install this update? RasBeacon restarts afterwards; connected devices are offline for about two minutes. If the new version doesn't start correctly, RasBeacon goes back to this one by itself.",
        )
      ) {
        void install();
      }
    },
    installing,
    setCheckOnline: (on) =>
      void run(
        async () => {
          await setCheckOnline(on);
          status.reload();
        },
        on ? "RasBeacon will check for new versions." : "Online check turned off.",
      ),
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
