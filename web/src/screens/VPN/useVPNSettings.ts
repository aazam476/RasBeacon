// Logic for the VPN page: import a WireGuard file, turn the VPN and its kill
// switch on or off, and (for advanced users) edit the details by hand.

import { useState } from "react";
import {
  ApiError,
  generateVPNKey,
  getVPNSettings,
  importVPNConfig,
  updateVPNSettings,
} from "../../api/client";
import type { VPNSettings } from "../../api/types";
import { useLoad } from "../../shared/useLoad";
import { fromForm, toForm, type VPNForm } from "./vpnForm";

export interface VPNSettingsLogic {
  saved: VPNSettings | undefined;
  loadError: string | null;

  // --- Importing a WireGuard file ---
  importText: string;
  setImportText: (text: string) => void;
  /** Reads a chosen .conf file into importText. */
  setImportFile: (file: File | null) => void;
  importConf: () => void;
  importing: boolean;
  importError: string | null;

  // --- The settings form ---
  form: VPNForm | undefined;
  setField: <K extends keyof VPNForm>(field: K, value: VPNForm[K]) => void;
  save: () => void;
  saving: boolean;
  saveError: string | null;
  /** The input the last error was about, e.g. "dns" (from the server). */
  errorField: string | null;
  /** Asks for confirmation, then makes a new key pair. */
  generateKey: () => void;

  /** Confirmation after any successful action. */
  message: string | null;
}

export function useVPNSettings(): VPNSettingsLogic {
  const loaded = useLoad(getVPNSettings);
  const [edits, setEdits] = useState<VPNForm | undefined>(undefined);
  const [importText, setImportText] = useState("");
  const [importing, setImporting] = useState(false);
  const [importError, setImportError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [errorField, setErrorField] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  const form = edits ?? (loaded.data ? toForm(loaded.data) : undefined);

  /** Runs an API call that returns new settings, with shared error handling. */
  async function act(
    call: () => Promise<VPNSettings>,
    done: string,
    onError: (msg: string) => void,
  ) {
    setMessage(null);
    setErrorField(null);
    try {
      await call();
      setEdits(undefined);
      setMessage(done);
      loaded.reload();
    } catch (err) {
      if (err instanceof ApiError && err.field) setErrorField(err.field);
      onError(err instanceof Error ? err.message : String(err));
    }
  }

  async function importConf() {
    setImporting(true);
    setImportError(null);
    await act(
      () => importVPNConfig(importText),
      'WireGuard file imported. Turn on "Use the VPN" below if it isn\'t already.',
      setImportError,
    );
    setImportText("");
    setImporting(false);
  }

  async function save() {
    if (!form || !loaded.data) return;
    setSaving(true);
    setSaveError(null);
    await act(() => updateVPNSettings(fromForm(form, loaded.data!)), "Saved.", setSaveError);
    setSaving(false);
  }

  async function generateKey() {
    if (
      !window.confirm(
        "Make a new key pair? The WireGuard server must then be given RasBeacon's new public key, or the VPN stops working.",
      )
    )
      return;
    setSaveError(null);
    await act(
      generateVPNKey,
      "New key pair made. Add the public key below to your WireGuard server.",
      setSaveError,
    );
  }

  return {
    saved: loaded.data,
    loadError: loaded.error,
    importText,
    setImportText: (text) => {
      setImportText(text);
      setImportError(null);
    },
    setImportFile: (file) => {
      if (file) void file.text().then(setImportText);
    },
    importConf: () => void importConf(),
    importing,
    importError,
    form,
    setField: (field, value) => {
      if (!form) return;
      setEdits({ ...form, [field]: value });
      setMessage(null);
    },
    save: () => void save(),
    saving,
    saveError,
    errorField,
    generateKey: () => void generateKey(),
    message,
  };
}
