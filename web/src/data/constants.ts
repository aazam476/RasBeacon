import type { Band } from './types';

/** Hotspot channels offered per band. They must match the router's list (allowedChannels in Go). */
export const HOTSPOT_CHANNELS: Record<Band, readonly number[]> = {
  '2.4GHz': [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11],
  '5GHz': [36, 40, 44, 48, 149, 153, 157, 161, 165],
};

/**
 * Where "Open the login page" goes when the router doesn't know the login page's address. Any
 * plain-HTTP site makes the network show its login page instead.
 */
export const FALLBACK_LOGIN_URL = 'http://neverssl.com/';

/**
 * The login page to open: the address the network redirected to, if it is a web address.
 * Anything else, such as a `javascript:` address a hostile network sent, would run as code on
 * RasBeacon's own page, so it is replaced by FALLBACK_LOGIN_URL.
 */
export function loginPageAddress(url: string | undefined): string {
  return url && /^https?:\/\//i.test(url) ? url : FALLBACK_LOGIN_URL;
}
