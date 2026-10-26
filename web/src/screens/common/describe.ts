// Router states in words, shared by the Status, Internet and VPN pages. The sentences come from
// the original UI; its browser test looks for some of them.
import type {
  EthernetUplinkStatus,
  HotspotStatus,
  Security,
  UplinkStatus,
  VPNStatus,
  WiFiUplinkState,
} from '@/data/types';
import type { BaseIconName, Tone } from '@/ui';

/** A one-line summary of the internet connection: `Connected to Wi-Fi "Corner Café"`. */
export function describeUplink(uplink: UplinkStatus): string {
  if (!uplink.connected) return 'Not connected';
  if (uplink.kind === 'ethernet') return 'Connected by Ethernet';
  return uplink.wifi.ssid ? `Connected to Wi-Fi "${uplink.wifi.ssid}"` : 'Connected by Wi-Fi';
}

/** What the radio that joins Wi-Fi networks is doing. */
export function describeWiFiState(state: WiFiUplinkState): string {
  switch (state) {
    case 'connected':
      return 'Connected';
    case 'connecting':
      return 'Connecting…';
    case 'searching':
      return 'Looking for your saved networks';
    case 'login-failed':
      return 'The network rejected the login: check the username, password, certificate and server name on the Internet page';
    case 'not-configured':
      return 'No networks saved (add one on the Internet page)';
    case 'off':
      return 'Off';
  }
}

/** Whether devices get IPv6, and whether through the VPN's public network. */
export function describeIpv6(uplink: UplinkStatus, vpn: VPNStatus): string {
  if (vpn.routedIpv6Prefix) return `Public addresses through the VPN (${vpn.routedIpv6Prefix})`;
  return uplink.ipv6 ? 'Yes' : 'No (IPv4 only)';
}

/** "Uplink, cable plugged in" */
export function describeEthernet(ethernet: EthernetUplinkStatus): string {
  return `${ethernet.role === 'uplink' ? 'Uplink' : 'Client port'}, ${ethernet.cablePlugged ? 'cable plugged in' : 'no cable'}`;
}

export function describeSecurity(security: Security): string {
  switch (security) {
    case 'wpa2-wpa3':
      return 'WPA2/WPA3';
    case 'wpa3':
      return 'WPA3 only';
    case 'wpa2':
      return 'WPA2 only';
  }
}

/** The metered setting, with what devices do about it. */
export function describeMetered(hotspot: HotspotStatus): string {
  return hotspot.metered ? 'On (Android follows it; set Low Data Mode on iPhones and Macs yourself)' : 'Off';
}

/** Whether RasBeacon is online, in a word or two, with the tone of its badge. */
export function uplinkBadge(uplink: UplinkStatus): { label: string; tone: Tone; pulse?: boolean } {
  if (uplink.connected) return { label: 'Online', tone: 'green' };
  if (uplink.wifi.available && uplink.wifi.state === 'connecting') {
    return { label: 'Connecting', tone: 'gray', pulse: true };
  }
  return { label: 'Not connected', tone: 'gray' };
}

export interface VpnStateView {
  /** The color of the state's symbol and badge. */
  tone: Tone;
  /** A shield: checked, crossed out, with an exclamation mark, or empty. */
  icon: BaseIconName;
  /** The state in a word or two, as the Status page's headline: "Protected". */
  title: string;
  /** The state for badges: "Connected", "No internet", … */
  badge: string;
  /** The full sentence. */
  summary: string;
  /** A failure that needs attention: announced when it appears. */
  alert: boolean;
  /** Still changing (the tunnel is starting). */
  pulse: boolean;
}

/**
 * The tunnel's state for display. The two failures stand out without looking like a crash:
 * "no internet because of the kill switch" in orange, "using the internet WITHOUT the VPN" in red.
 */
export function vpnStateView(vpn: VPNStatus): VpnStateView {
  const state = vpn.state === 'blocked' ? 'blocked' : vpn.enabled ? vpn.state : 'off';
  switch (state) {
    case 'up':
      return {
        tone: 'green',
        icon: 'verified_user',
        title: 'Protected',
        badge: 'Connected',
        summary: "Connected: your devices' traffic goes through the VPN.",
        alert: false,
        pulse: false,
      };
    case 'connecting':
    case 'down':
      if (vpn.killSwitch) {
        return {
          tone: 'orange',
          icon: 'gpp_maybe',
          title: state === 'connecting' ? 'Connecting…' : 'No internet',
          badge: state === 'connecting' ? 'Connecting' : 'No internet',
          summary: 'Not connected. The kill switch is on, so your devices have no internet until the VPN works.',
          alert: state === 'down',
          pulse: state === 'connecting',
        };
      }
      return {
        tone: 'gray',
        icon: 'shield',
        title: 'Connecting…',
        badge: 'Connecting',
        summary: 'Not connected yet. If it stays down for 30 seconds, devices use the internet without the VPN.',
        alert: false,
        pulse: true,
      };
    case 'direct':
      return {
        tone: 'red',
        icon: 'gpp_bad',
        title: 'Not protected',
        badge: 'Without VPN',
        summary: 'Not connected: your devices are using the internet WITHOUT the VPN (kill switch off).',
        alert: true,
        pulse: false,
      };
    case 'blocked':
      return {
        tone: 'red',
        icon: 'gpp_bad',
        title: 'No internet',
        badge: 'Blocked',
        summary: 'Devices have no internet: the kill switch is on and the VPN is off.',
        alert: true,
        pulse: false,
      };
    case 'off':
      return {
        tone: 'gray',
        icon: 'shield',
        title: 'VPN off',
        badge: 'Off',
        summary: 'Off: your devices use the internet directly.',
        alert: false,
        pulse: false,
      };
  }
}

/**
 * The short reason shown in place of a feature's subtitle when it needs the internet and
 * RasBeacon has none right now. rbd refuses those features with its own message too.
 */
export const NEEDS_INTERNET = 'Needs an internet connection';

/**
 * Why "Use the VPN" can't be turned on right now, or null when it can: the tunnel can't start
 * without the internet. Turning the VPN off always works. (Without a profile the switch is grayed
 * out as well; the pages handle that case themselves.)
 */
export function vpnSwitchBlocked(vpn: VPNStatus, online: boolean): string | null {
  return !vpn.enabled && !online ? NEEDS_INTERNET : null;
}
