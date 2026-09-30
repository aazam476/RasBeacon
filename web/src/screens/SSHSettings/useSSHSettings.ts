// Logic for the SSH settings page. SSH is for advanced users: it is off by
// default and only allows public-key login from the hotspot side.

import { useState } from "react";
import { getSSHSettings, updateSSHSettings } from "../../api/client";
import type { SSHSettings } from "../../api/types";
import { useLoad } from "../../shared/useLoad";

export interface SSHSettingsLogic {
  values: SSHSettings | undefined;
  setEnabled: (enabled: boolean) => void;
  setAuthorizedKeys: (keys: string) => void;
  loadError: string | null;
  saveError: string | null;
  saving: boolean;
  saved: boolean;
  save: () => void;
}

export function useSSHSettings(): SSHSettingsLogic {
  const loaded = useLoad(getSSHSettings);
  const [edits, setEdits] = useState<SSHSettings | undefined>(undefined);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  const values = edits ?? loaded.data;

  function change(patch: Partial<SSHSettings>) {
    if (!values) return;
    setEdits({ ...values, ...patch });
    setSaved(false);
  }

  async function save() {
    if (!values) return;
    setSaving(true);
    setSaveError(null);
    try {
      await updateSSHSettings(values);
      setEdits(undefined);
      setSaved(true);
      loaded.reload();
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return {
    values,
    setEnabled: (enabled) => change({ enabled }),
    setAuthorizedKeys: (authorizedKeys) => change({ authorizedKeys }),
    loadError: loaded.error,
    saveError,
    saving,
    saved,
    save: () => void save(),
  };
}
