// Moving this window to another page or host. In their own module so the data layer's tests can
// watch them (jsdom can't really navigate).

/** Opens address in this window, as following a link would. */
export function openAddress(address: string) {
  window.location.assign(address);
}

/** The host this page was opened from, such as "rasbeacon.lan". */
export function currentHostname() {
  return window.location.hostname;
}

/** Moves this window to the same page on the host named `to` instead of `from`, keeping any port. */
export function renameHost(from: string, to: string) {
  window.location.host = window.location.host.replace(from, to);
}
