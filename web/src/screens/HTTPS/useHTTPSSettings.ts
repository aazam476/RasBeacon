// Logic for the HTTPS page: turn HTTPS on or off for these settings pages,
// make a new certificate, or upload your own.

import { useState } from "react";
import { getHTTPSSettings, getSetupState, updateHTTPSSettings } from "../../api/client";
import type { HTTPSSettings } from "../../api/types";
import { useLoad } from "../../shared/useLoad";

export interface HTTPSSettingsLogic {
  settings: HTTPSSettings | undefined;
  loadError: string | null;
  /** Where the settings will be once HTTPS is on, e.g. https://10.87.0.1/. */
  httpsAddress: string;
  /** True when this page itself was opened over HTTPS. */
  onHTTPS: boolean;
  setEnabled: (enabled: boolean) => void;
  /** Asks for confirmation, then makes a new certificate. */
  generateNew: () => void;
  certificateText: string;
  setCertificateText: (text: string) => void;
  keyText: string;
  setKeyText: (text: string) => void;
  /** Reads a chosen file into the certificate or key box. */
  setFile: (which: "certificate" | "key", file: File | null) => void;
  upload: () => void;
  busy: boolean;
  error: string | null;
  message: string | null;
}

/** Loads the settings and RasBeacon's address together. */
async function loadAll() {
  const [settings, state] = await Promise.all([getHTTPSSettings(), getSetupState()]);
  return { settings, lanAddress: state.lanAddress };
}

export function useHTTPSSettings(): HTTPSSettingsLogic {
  const loaded = useLoad(loadAll);
  const [certificateText, setCertificateText] = useState("");
  const [keyText, setKeyText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const settings = loaded.data?.settings;
  const httpsAddress = `https://${loaded.data?.lanAddress ?? "10.87.0.1"}/`;

  async function save(change: Partial<HTTPSSettings>, done: string) {
    if (!settings) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      await updateHTTPSSettings({ enabled: settings.enabled, ...change } as HTTPSSettings);
      setMessage(done);
      setCertificateText("");
      setKeyText("");
      loaded.reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return {
    settings,
    loadError: loaded.error,
    httpsAddress,
    onHTTPS: window.location.protocol === "https:",
    setEnabled: (enabled) =>
      void save(
        { enabled },
        enabled
          ? `HTTPS is on. In a few seconds, continue at ${httpsAddress} (your browser will warn about the certificate: compare the fingerprint below).`
          : "HTTPS is off.",
      ),
    generateNew: () => {
      if (window.confirm("Make a new certificate? Browsers will warn about it again once.")) {
        void save({ generateNew: true }, "New certificate made.");
      }
    },
    certificateText,
    setCertificateText,
    keyText,
    setKeyText,
    setFile: (which, file) => {
      if (!file) return;
      void file.text().then(which === "certificate" ? setCertificateText : setKeyText);
    },
    upload: () =>
      void save({ certificate: certificateText, privateKey: keyText }, "Certificate uploaded."),
    busy,
    error,
    message,
  };
}
