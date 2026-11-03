/*
 * The router's input checks that only the demo needs, so it can show those error states too:
 * the ones the real provider leaves to the router (a country code, a WireGuard file, the
 * channels a country allows). The checks both share (names, passwords, the forms) are the real
 * provider's own, in data/real/logic.ts.
 */
import { HOTSPOT_CHANNELS } from '../constants';
import { FieldErrors } from '../errors';
import { checkSsid, checkWifiPassword } from '../real/logic';
import type { ChannelsByBand, HotspotSettings } from '../types';

/** Error messages by field name, as the router answers them. */
type Errors = Record<string, string>;

/**
 * The hotspot form: the name and password as the real provider checks them, and the country
 * code as the router refuses it (the real provider leaves that to the router).
 */
export function checkHotspotSettings(settings: HotspotSettings): Errors {
  const errors: Errors = {};
  const ssid = checkSsid(settings.ssid);
  if (ssid) errors.ssid = ssid;
  if (settings.password) {
    const password = checkWifiPassword(settings.password);
    if (password) errors.password = password;
  }
  // As the router says it: "UK" isn't a country code, the United Kingdom's is GB.
  if (settings.country === 'UK') errors.country = 'Use GB for the United Kingdom.';
  else if (!/^[A-Z]{2}$/.test(settings.country)) errors.country = 'Must be a two-letter country code such as US.';
  return errors;
}

/** The router refuses WireGuard files that would run commands. */
export function checkWireGuardFile(text: string): string | null {
  const lines = text.split('\n');
  const index = lines.findIndex((line) => /^\s*(PreUp|PostUp|PreDown|PostDown)\s*=/i.test(line));
  if (index >= 0) {
    const key = lines[index].split('=')[0].trim();
    return `Line ${index + 1}: RasBeacon never runs commands from WireGuard files (${key}). Remove that line.`;
  }
  if (!/^\s*\[Interface\]/im.test(text) || !/^\s*\[Peer\]/im.test(text)) {
    return 'This is not a WireGuard file: it needs an [Interface] and a [Peer] section.';
  }
  return null;
}

/**
 * Like the router's regulatory database, for the countries the demo knows well: Japan has no
 * channels from 149 up, and a code that isn't a country is refused as the router refuses it.
 */
export function demoHotspotChannels(country: string): ChannelsByBand {
  const problem = checkHotspotSettings({
    ssid: 'x',
    band: '2.4GHz',
    channel: 0,
    security: 'wpa2-wpa3',
    country,
    metered: false,
  });
  if (problem.country) throw new FieldErrors({ country: problem.country });
  const fiveGHz = HOTSPOT_CHANNELS['5GHz'].filter((channel) => country !== 'JP' || channel < 149);
  // As Linux's regulatory database has it: no 6 GHz access point in the US; Europe, the UK and
  // Japan allow the lower part of the band (up to 6425 MHz).
  const sixGHz = country === 'US' ? [] : HOTSPOT_CHANNELS['6GHz'].filter((channel) => 5950 + 5 * channel <= 6415);
  return { '2.4GHz': [...HOTSPOT_CHANNELS['2.4GHz']], '5GHz': fiveGHz, '6GHz': sixGHz };
}
