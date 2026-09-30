// Logic for the hotspot settings page: name, password, band, channel,
// security and country.

import { useState } from "react";
import {
  ApiError,
  getHotspotSettings,
  revealHotspotPassword,
  updateHotspotSettings,
} from "../../api/client";
import type { HotspotSettings } from "../../api/types";
import { useLoad } from "../../shared/useLoad";
import { checkSsid, checkWifiPassword } from "../../shared/validation";

export type HotspotField = keyof HotspotSettings;

export interface HotspotSettingsLogic {
  /** The form values, or undefined while the current settings load. */
  values: HotspotSettings | undefined;
  setField: <K extends HotspotField>(field: K, value: HotspotSettings[K]) => void;
  errors: Partial<Record<HotspotField, string>>;
  loadError: string | null;
  saveError: string | null;
  saving: boolean;
  /** True briefly after a successful save. */
  saved: boolean;
  save: () => void;

  /** Revealing the current password asks for the admin password first. */
  revealAdminPassword: string;
  setRevealAdminPassword: (value: string) => void;
  revealedPassword: string | null;
  revealError: string | null;
  reveal: () => void;
}

export function useHotspotSettings(): HotspotSettingsLogic {
  const loaded = useLoad(getHotspotSettings);
  // `edits` holds what the owner changed; undefined means "no changes yet,
  // show the loaded settings".
  const [edits, setEdits] = useState<HotspotSettings | undefined>(undefined);
  const [errors, setErrors] = useState<Partial<Record<HotspotField, string>>>({});
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  const [revealAdminPassword, setRevealAdminPassword] = useState("");
  const [revealedPassword, setRevealedPassword] = useState<string | null>(null);
  const [revealError, setRevealError] = useState<string | null>(null);

  const values = edits ?? (loaded.data ? { ...loaded.data, password: "" } : undefined);

  const setField: HotspotSettingsLogic["setField"] = (field, value) => {
    if (!values) return;
    const next = { ...values, [field]: value };
    // Switching band resets the channel to that band's default.
    if (field === "band") next.channel = 0;
    setEdits(next);
    setErrors((current) => ({ ...current, [field]: undefined }));
    setSaved(false);
  };

  async function save() {
    if (!values) return;
    const newErrors: Partial<Record<HotspotField, string>> = {};
    const ssidError = checkSsid(values.ssid);
    if (ssidError) newErrors.ssid = ssidError;
    if (values.password) {
      const passwordError = checkWifiPassword(values.password);
      if (passwordError) newErrors.password = passwordError;
    }
    setErrors(newErrors);
    setSaveError(null);
    if (Object.keys(newErrors).length > 0) return;

    setSaving(true);
    try {
      await updateHotspotSettings(values);
      setEdits(undefined);
      setSaved(true);
      loaded.reload();
    } catch (err) {
      if (err instanceof ApiError && err.field) {
        // Server field names look like "hotspot.ssid"; keep the last part.
        const field = err.field.replace("hotspot.", "") as HotspotField;
        setErrors({ [field]: err.message });
      } else {
        setSaveError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  }

  async function reveal() {
    setRevealError(null);
    try {
      const response = await revealHotspotPassword(revealAdminPassword);
      setRevealedPassword(response.hotspotPassword);
      setRevealAdminPassword("");
    } catch (err) {
      setRevealError(err instanceof Error ? err.message : String(err));
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
    revealAdminPassword,
    setRevealAdminPassword,
    revealedPassword,
    revealError,
    reveal: () => void reveal(),
  };
}
