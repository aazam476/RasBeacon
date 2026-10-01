// Pure helpers for the Internet page's list of saved Wi-Fi networks. They
// don't change anything in place: each returns a new list, which is what
// React state needs. Kept separate from the hook so they can be unit-tested.

import type { EAPMethod, NearbyNetwork, SavedNetwork, UplinkSecurity } from "../../api/types";
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

/** An uploaded certificate or key: its file name and its contents for the API. */
export interface UploadedFile {
  name: string;
  /** PEM text, or base64-encoded binary (DER). */
  content: string;
}

/** The "add a network" form. */
export interface NewNetworkForm {
  ssid: string;
  security: UplinkSecurity;
  /** Network password; for Enterprise, your password or (EAP-TLS) key passphrase. */
  password: string;
  hidden: boolean;
  // --- Enterprise only ---
  method: EAPMethod;
  identity: string;
  anonymousIdentity: string;
  serverName: string;
  caCert: UploadedFile | null;
  clientCert: UploadedFile | null;
  clientKey: UploadedFile | null;
  allowNoCertificateCheck: boolean;
}

export const EMPTY_NEW_NETWORK: NewNetworkForm = {
  ssid: "",
  security: "personal",
  password: "",
  hidden: false,
  method: "ttls-pap",
  identity: "",
  anonymousIdentity: "",
  serverName: "",
  caCert: null,
  clientCert: null,
  clientKey: null,
  allowNoCertificateCheck: false,
};

/**
 * The eduroam preset: name and the most common login method filled in.
 * Your institution tells you the rest (see docs/eduroam.md).
 */
export const EDUROAM_FORM: NewNetworkForm = {
  ...EMPTY_NEW_NETWORK,
  ssid: "eduroam",
  security: "enterprise",
  method: "ttls-pap",
};

export type NewNetworkErrors = Partial<Record<keyof NewNetworkForm, string>>;

/** Server names like "radius.example.edu" or "*.example.edu". */
const SERVER_NAME = /^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$/i;

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
  if (form.security === "enterprise") {
    if (!form.identity) errors.identity = "Enter your username.";
    if (form.method === "tls") {
      if (!form.clientCert) errors.clientCert = "Choose your personal certificate file.";
      if (!form.clientKey) errors.clientKey = "Choose the key file for your certificate.";
    } else if (!form.password) {
      errors.password = "Enter your password.";
    }
    if (!form.allowNoCertificateCheck) {
      if (!form.caCert)
        errors.caCert = "Choose the network's CA certificate file (your institution publishes it).";
      if (!form.serverName) errors.serverName = "Enter the login server's name.";
      else if (!SERVER_NAME.test(form.serverName.trim()))
        errors.serverName = "Must look like radius.example.edu or *.example.edu.";
    }
  }
  return errors;
}

/** Turns the form into a list entry. New networks have no ID yet. */
export function toSavedNetwork(form: NewNetworkForm): SavedNetwork {
  const network: SavedNetwork = {
    id: "",
    ssid: form.ssid,
    security: form.security,
    hidden: form.hidden,
    hasPassword: false,
    password: form.security === "open" ? "" : form.password,
  };
  if (form.security === "enterprise") {
    network.enterprise = {
      method: form.method,
      identity: form.identity,
      anonymousIdentity: form.anonymousIdentity,
      serverName: form.serverName.trim(),
      allowNoCertificateCheck: form.allowNoCertificateCheck,
      caCert: form.caCert?.content ?? "",
      caCertSummary: form.caCert ? `${form.caCert.name} (not saved yet)` : "",
      clientCert: form.method === "tls" ? (form.clientCert?.content ?? "") : "",
      clientCertSummary: "",
      clientKey: form.method === "tls" ? (form.clientKey?.content ?? "") : "",
      hasClientKey: false,
    };
  }
  return network;
}

/**
 * Fills the form from a network found by a scan. Returns an explanation
 * instead when RasBeacon can't join that kind of network.
 */
export function formFromNearby(network: NearbyNetwork): NewNetworkForm | string {
  switch (network.security) {
    case "open":
    case "personal":
    case "enterprise":
      return { ...EMPTY_NEW_NETWORK, ssid: network.ssid, security: network.security };
    default:
      return `"${network.ssid}" uses ${network.label || "an old or unusual kind of security"}, which RasBeacon does not support.`;
  }
}

/**
 * Prepares an uploaded file for the API: PEM files are sent as text, binary
 * (DER) files as base64.
 */
export function fileToUpload(name: string, bytes: Uint8Array): UploadedFile {
  const text = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  if (text.includes("-----BEGIN")) return { name, content: text };
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return { name, content: btoa(binary) };
}

/**
 * Combines one or more certificate files into one PEM upload. Binary (DER)
 * certificates are converted to PEM, so a bundle can mix both kinds. Some
 * networks need several CA certificates (Rutgers lists two).
 */
export function certificatesToUpload(files: { name: string; bytes: Uint8Array }[]): UploadedFile {
  const parts = files.map(({ name, bytes }) => {
    const upload = fileToUpload(name, bytes);
    if (upload.content.includes("-----BEGIN")) return upload.content.trim() + "\n";
    const lines = upload.content.match(/.{1,64}/g) ?? [];
    return ["-----BEGIN CERTIFICATE-----", ...lines, "-----END CERTIFICATE-----", ""].join("\n");
  });
  return { name: files.map((f) => f.name).join(", "), content: parts.join("") };
}

/**
 * The server names rejected inputs like "networks.2.password" or
 * "networks.0.enterprise.caCert". This splits that into the list position
 * and the input, or returns null for other fields.
 */
export function networkErrorField(
  field: string | undefined,
): { index: number; input: string } | null {
  const match = /^networks\.(\d+)\.([\w.]+)$/.exec(field ?? "");
  return match ? { index: Number(match[1]), input: match[2] } : null;
}
