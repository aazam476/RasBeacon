// Logic for the Internet page: what the Ethernet port is for, and which
// upstream Wi-Fi networks RasBeacon joins (in order of preference).
//
// Changes are collected locally and sent together with "Save", because the
// server stores the whole list at once (see PUT /api/v1/settings/uplink).

import { useState } from "react";
import {
  ApiError,
  getUplinkSettings,
  scanNearbyNetworks,
  updateUplinkSettings,
} from "../../api/client";
import type { EthernetRole, NearbyNetwork, SavedNetwork, UplinkSettings } from "../../api/types";
import { useLoad } from "../../shared/useLoad";
import {
  checkNewNetwork,
  EMPTY_NEW_NETWORK,
  formFromNearby,
  moveNetwork,
  networkErrorField,
  removeNetwork,
  setNetworkPassword,
  toSavedNetwork,
  type NewNetworkErrors,
  type NewNetworkForm,
} from "./networkList";

export interface UplinkSettingsLogic {
  /** The settings being edited, or undefined while they load. */
  values: UplinkSettings | undefined;
  loadError: string | null;

  setEthernetRole: (role: EthernetRole) => void;

  // --- Saved networks ---
  moveUp: (index: number) => void;
  moveDown: (index: number) => void;
  remove: (index: number) => void;
  /** Sets a new password for a saved network ("" keeps the saved one). */
  setPassword: (index: number, password: string) => void;
  /** Errors from the server, by list position. */
  networkErrors: Record<number, string>;

  // --- Adding a network ---
  newNetwork: NewNetworkForm;
  setNewNetworkField: <K extends keyof NewNetworkForm>(field: K, value: NewNetworkForm[K]) => void;
  newNetworkErrors: NewNetworkErrors;
  /** Adds the form's network to the end of the list (saved with "Save"). */
  addNetwork: () => void;

  // --- Scanning for nearby networks ---
  scan: () => void;
  scanning: boolean;
  nearby: NearbyNetwork[] | null;
  scanError: string | null;
  /** Fills the "add" form from a scan result. */
  pickNearby: (network: NearbyNetwork) => void;

  // --- Saving ---
  /** True when there are changes that have not been saved. */
  dirty: boolean;
  save: () => void;
  saving: boolean;
  saved: boolean;
  saveError: string | null;
}

export function useUplinkSettings(): UplinkSettingsLogic {
  const loaded = useLoad(getUplinkSettings);
  const [edits, setEdits] = useState<UplinkSettings | undefined>(undefined);
  const [networkErrors, setNetworkErrors] = useState<Record<number, string>>({});
  const [newNetwork, setNewNetwork] = useState<NewNetworkForm>(EMPTY_NEW_NETWORK);
  const [newNetworkErrors, setNewNetworkErrors] = useState<NewNetworkErrors>({});
  const [scanning, setScanning] = useState(false);
  const [nearby, setNearby] = useState<NearbyNetwork[] | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const values = edits ?? loaded.data;

  /** Applies a change to the settings being edited. */
  function change(update: (current: UplinkSettings) => UplinkSettings) {
    if (!values) return;
    setEdits(update(values));
    setNetworkErrors({});
    setSaved(false);
  }

  function changeNetworks(update: (networks: SavedNetwork[]) => SavedNetwork[]) {
    change((current) => ({ ...current, networks: update(current.networks) }));
  }

  function addNetwork() {
    if (!values) return;
    const errors = checkNewNetwork(newNetwork, values.networks);
    setNewNetworkErrors(errors);
    if (Object.keys(errors).length > 0) return;
    changeNetworks((networks) => [...networks, toSavedNetwork(newNetwork)]);
    setNewNetwork(EMPTY_NEW_NETWORK);
  }

  async function scan() {
    setScanning(true);
    setScanError(null);
    try {
      setNearby((await scanNearbyNetworks()).networks);
    } catch (err) {
      setScanError(err instanceof Error ? err.message : String(err));
    } finally {
      setScanning(false);
    }
  }

  function pickNearby(network: NearbyNetwork) {
    const result = formFromNearby(network);
    if (typeof result === "string") {
      setScanError(result);
      return;
    }
    setScanError(null);
    setNewNetwork(result);
    setNewNetworkErrors({});
  }

  async function save() {
    if (!values) return;
    setSaving(true);
    setSaveError(null);
    setNetworkErrors({});
    try {
      await updateUplinkSettings(values);
      setEdits(undefined);
      setSaved(true);
      loaded.reload();
    } catch (err) {
      const where = err instanceof ApiError ? networkErrorField(err.field) : null;
      if (err instanceof ApiError && where) {
        setNetworkErrors({ [where.index]: err.message });
      } else {
        setSaveError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setSaving(false);
    }
  }

  return {
    values,
    loadError: loaded.error,
    setEthernetRole: (role) => change((current) => ({ ...current, ethernetRole: role })),
    moveUp: (index) => changeNetworks((networks) => moveNetwork(networks, index, -1)),
    moveDown: (index) => changeNetworks((networks) => moveNetwork(networks, index, 1)),
    remove: (index) => changeNetworks((networks) => removeNetwork(networks, index)),
    setPassword: (index, password) =>
      changeNetworks((networks) => setNetworkPassword(networks, index, password)),
    networkErrors,
    newNetwork,
    setNewNetworkField: (field, value) => {
      setNewNetwork((current) => ({ ...current, [field]: value }));
      setNewNetworkErrors((current) => ({ ...current, [field]: undefined }));
    },
    newNetworkErrors,
    addNetwork,
    scan: () => void scan(),
    scanning,
    nearby,
    scanError,
    pickNearby,
    dirty: edits !== undefined,
    save: () => void save(),
    saving,
    saved,
    saveError,
  };
}
