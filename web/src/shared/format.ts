// Small helpers that turn numbers from the API into words for the screen.

/** 3725 → "1 h 2 min"; 42 → "42 s". */
export function formatDuration(totalSeconds: number): string {
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = Math.floor(totalSeconds % 60);

  if (days > 0) return `${days} d ${hours} h`;
  if (hours > 0) return `${hours} h ${minutes} min`;
  if (minutes > 0) return `${minutes} min`;
  return `${seconds} s`;
}

/**
 * Wi-Fi signal strength in words. dBm values are negative; closer to zero
 * is stronger (-45 is excellent, -80 is poor).
 */
export function describeSignal(dbm: number): string {
  if (dbm === 0) return "unknown";
  if (dbm >= -55) return `excellent (${dbm} dBm)`;
  if (dbm >= -67) return `good (${dbm} dBm)`;
  if (dbm >= -75) return `fair (${dbm} dBm)`;
  return `poor (${dbm} dBm)`;
}

/** 2437 → "2.4 GHz"; 5180 → "5 GHz". */
export function describeBand(frequencyMhz: number): string {
  if (frequencyMhz <= 0) return "unknown band";
  if (frequencyMhz < 3000) return "2.4 GHz";
  if (frequencyMhz < 5925) return "5 GHz";
  return "6 GHz";
}

/** A one-line summary of the internet connection for the status page. */
export function describeUplink(uplink: {
  connected: boolean;
  kind: string;
  wifi: { ssid: string };
}): string {
  if (!uplink.connected) return "Not connected";
  if (uplink.kind === "ethernet") return "Connected by Ethernet";
  return uplink.wifi.ssid ? `Connected to Wi-Fi "${uplink.wifi.ssid}"` : "Connected by Wi-Fi";
}

/** Explains a tunnel state to the owner. */
export function describeVPNState(state: string, killSwitch: boolean): string {
  switch (state) {
    case "up":
      return "Connected: your devices' traffic goes through the VPN.";
    case "connecting":
    case "down":
      return killSwitch
        ? "Not connected. The kill switch is on, so your devices have no internet until the VPN works."
        : "Not connected yet. If it stays down for 30 seconds, devices use the internet without the VPN.";
    case "direct":
      return "Not connected: your devices are using the internet WITHOUT the VPN (kill switch off).";
    default:
      return "Off: your devices use the internet directly.";
  }
}
