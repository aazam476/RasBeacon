// Pure helpers for the Internet page's list of saved Wi-Fi networks. They
// don't change anything in place: each returns a new list, which is what
// React state needs. Kept separate from the hook so they can be unit-tested.

import type { NearbyNetwork, SavedNetwork, UplinkSecurity } from "../../api/types";
import { checkSsid, checkWifiPassword } from "../../shared/validation";

/** Moves the network at `index` up (delta -1) or down (delta +1). */
export function moveNetwork(list: SavedNetwork[], index: number, delta: -1 | 1): SavedNetwork[] {
  const target = index + delta;
  if (target < 0 || target >= list.length) return list;
  const next = [...list];
  [next[index], next[target]] = [next[target], next[index]];
  return next;
}

/** Removes the network at `index`. */
export function removeNetwork(list: SavedNetwork[], index: number): SavedNetwork[] {
  return list.filter((_, i) => i !== index);
}

/** Changes the password of the network at `index` ("" = keep the saved one). */
export function setNetworkPassword(
  list: SavedNetwork[],
  index: number,
  password: string,
): SavedNetwork[] {
  return list.map((network, i) => (i === index ? { ...network, password } : network));
}

/** The "add a network" form. */
export interface NewNetworkForm {
  ssid: string;
  security: UplinkSecurity;
  password: string;
  hidden: boolean;
}

export const EMPTY_NEW_NETWORK: NewNetworkForm = {
  ssid: "",
  security: "personal",
  password: "",
  hidden: false,
};

export type NewNetworkErrors = Partial<Record<keyof NewNetworkForm, string>>;

/** Checks the "add a network" form. An empty result means it is valid. */
export function checkNewNetwork(form: NewNetworkForm, existing: SavedNetwork[]): NewNetworkErrors {
  const errors: NewNetworkErrors = {};
  const ssidError = checkSsid(form.ssid);
  if (ssidError) errors.ssid = ssidError;
  else if (existing.some((n) => n.ssid === form.ssid))
    errors.ssid = "This network is already in the list.";
  if (form.security === "personal") {
    const passwordError = checkWifiPassword(form.password);
    if (passwordError) errors.password = passwordError;
  }
  return errors;
}

/** Turns the form into a list entry. New networks have no ID yet. */
export function toSavedNetwork(form: NewNetworkForm): SavedNetwork {
  return {
    id: "",
    ssid: form.ssid,
    security: form.security,
    hidden: form.hidden,
    hasPassword: false,
    password: form.security === "personal" ? form.password : "",
  };
}

/**
 * Fills the form from a network found by a scan. Returns an explanation
 * instead when RasBeacon can't join that kind of network yet.
 */
export function formFromNearby(network: NearbyNetwork): NewNetworkForm | string {
  switch (network.security) {
    case "open":
    case "personal":
      return { ...EMPTY_NEW_NETWORK, ssid: network.ssid, security: network.security };
    case "enterprise":
      return `"${network.ssid}" needs a username and certificate (WPA-Enterprise, like eduroam). Support for these networks is coming in the next update.`;
    default:
      return `"${network.ssid}" uses an old or unusual kind of security that RasBeacon does not support.`;
  }
}

/**
 * The server names rejected inputs like "networks.2.password". This splits
 * that into the list position and the input, or returns null for other
 * fields.
 */
export function networkErrorField(
  field: string | undefined,
): { index: number; input: string } | null {
  const match = /^networks\.(\d+)\.(\w+)$/.exec(field ?? "");
  return match ? { index: Number(match[1]), input: match[2] } : null;
}
