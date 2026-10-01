// Pure helpers for the VPN page: the form shows address lists as one line
// of comma-separated text, the API uses arrays.

import type { VPNSettings } from "../../api/types";

/** The VPN form. Lists are comma-separated text. */
export interface VPNForm {
  enabled: boolean;
  killSwitch: boolean;
  addresses: string;
  dns: string;
  endpoint: string;
  peerPublicKey: string;
  allowedIps: string;
  persistentKeepalive: string;
  mtu: string;
  /** New keys (empty = keep the saved one). */
  privateKey: string;
  presharedKey: string;
}

/** Fills the form from the saved settings. */
export function toForm(s: VPNSettings): VPNForm {
  return {
    enabled: s.enabled,
    killSwitch: s.killSwitch,
    addresses: s.addresses.join(", "),
    dns: s.dns.join(", "),
    endpoint: s.endpoint,
    peerPublicKey: s.peerPublicKey,
    allowedIps: s.allowedIps.length > 0 ? s.allowedIps.join(", ") : "0.0.0.0/0, ::/0",
    persistentKeepalive: s.persistentKeepalive ? String(s.persistentKeepalive) : "",
    mtu: s.mtu ? String(s.mtu) : "",
    privateKey: "",
    presharedKey: "",
  };
}

/** Splits "a, b,c" into ["a", "b", "c"]. */
export function splitList(text: string): string[] {
  return text
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item !== "");
}

/** Turns the form back into API settings. Empty numbers mean "default" (0). */
export function fromForm(form: VPNForm, saved: VPNSettings): VPNSettings {
  return {
    ...saved,
    enabled: form.enabled,
    killSwitch: form.killSwitch,
    addresses: splitList(form.addresses),
    dns: splitList(form.dns),
    endpoint: form.endpoint.trim(),
    peerPublicKey: form.peerPublicKey.trim(),
    allowedIps: splitList(form.allowedIps),
    persistentKeepalive: Number(form.persistentKeepalive) || 0,
    mtu: Number(form.mtu) || 0,
    privateKey: form.privateKey.trim(),
    presharedKey: form.presharedKey.trim(),
  };
}
