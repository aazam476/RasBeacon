// Logic for the guest network page: a second Wi-Fi network for visitors.
// Guests can use the internet (through the VPN, if it is on) but can't
// reach your devices, each other, or these settings.

import { useState } from "react";
import { ApiError, getGuestSettings, updateGuestSettings } from "../../api/client";
import type { GuestSettings } from "../../api/types";
import { useLoad } from "../../shared/useLoad";
import { checkSsid, checkWifiPassword } from "../../shared/validation";

export type GuestField = "enabled" | "ssid" | "password";

export interface GuestSettingsLogic {
  /** The form values, or undefined while the settings load. */
  values: GuestSettings | undefined;
  setField: <K extends GuestField>(field: K, value: GuestSettings[K]) => void;
  errors: Partial<Record<GuestField, string>>;
  loadError: string | null;
  saveError: string | null;
  saving: boolean;
  /** True briefly after a successful save. */
  saved: boolean;
  save: () => void;
}

/** Checks the form before sending it; returns errors by field. */
export function checkGuestForm(values: GuestSettings): Partial<Record<GuestField, string>> {
  const errors: Partial<Record<GuestField, string>> = {};
  if (!values.enabled) return errors;
  const ssidError = checkSsid(values.ssid);
  if (ssidError) errors.ssid = ssidError;
  if (values.password) {
    const passwordError = checkWifiPassword(values.password);
    if (passwordError) errors.password = passwordError;
  } else if (!values.hasPassword) {
    errors.password = "Choose a password for the guest network.";
  }
  return errors;
}

export function useGuestSettings(): GuestSettingsLogic {
  const loaded = useLoad(getGuestSettings);
  const [edits, setEdits] = useState<GuestSettings | undefined>(undefined);
  const [errors, setErrors] = useState<Partial<Record<GuestField, string>>>({});
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  const values = edits ?? (loaded.data ? { ...loaded.data, password: "" } : undefined);

  const setField: GuestSettingsLogic["setField"] = (field, value) => {
    if (!values) return;
    setEdits({ ...values, [field]: value });
    setErrors((current) => ({ ...current, [field]: undefined }));
    setSaved(false);
  };

  async function save() {
    if (!values) return;
    const newErrors = checkGuestForm(values);
    setErrors(newErrors);
    setSaveError(null);
    if (Object.keys(newErrors).length > 0) return;
    setSaving(true);
    try {
      await updateGuestSettings(values);
      setEdits(undefined);
      setSaved(true);
      loaded.reload();
    } catch (err) {
      if (
        err instanceof ApiError &&
        (err.field === "ssid" || err.field === "password" || err.field === "enabled")
      ) {
        setErrors({ [err.field]: err.message });
      } else {
        setSaveError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  }

  return {
    values,
    setField,
    errors,
    loadError: loaded.error,
    saveError,
    saving,
    saved,
    save: () => void save(),
  };
}
