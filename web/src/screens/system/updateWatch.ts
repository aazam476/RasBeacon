import { useEffect, useState } from 'react';
import type { Status } from '@/data/types';
import { reloadPage } from '@/lib/navigate';

/**
 * Following an update through RasBeacon's restart, for the System page. While an update installs
 * or RasBeacon restarts into it (`update.restarting`), from this page or elsewhere, the page notes
 * which version was running. RasBeacon can't be reached for a couple of minutes; when it answers
 * again with another version, the update worked: the page loads again (the new version may bring
 * a new web UI) and says "Update completed". When the update failed instead, RasBeacon says why
 * (`update.failed`), and the page stops waiting.
 *
 * The notes are kept in sessionStorage, so they survive the reload but not the tab.
 */

/** The version that ran when an update started: set while the page waits for the restart. */
const UPDATING_FROM = 'rasbeacon.update.from';
/** The version an update ended on, kept across the reload for the "Update completed" line. */
const UPDATED_TO = 'rasbeacon.update.to';

/** Reads a note; null when there is none or storage is blocked (a private window). */
function readNote(key: string): string | null {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Writes a note (null removes it); does nothing when storage is blocked. */
function writeNote(key: string, value: string | null) {
  try {
    if (value === null) sessionStorage.removeItem(key);
    else sessionStorage.setItem(key, value);
  } catch {
    // Without storage the page just doesn't follow the update.
  }
}

/** Notes that an update starts now, from `version`: the page then waits for the restart. */
export function noteUpdateStarted(version: string) {
  writeNote(UPDATING_FROM, version);
}

/**
 * Follows an update through the restart (see above), from the status as it refreshes. Returns the
 * version an update just finished on, for "Update completed", or null.
 */
export function useUpdateWatch(status: Status): string | null {
  const { version, update } = status;
  // Read once: the line stays while the page is open, and goes with the next visit.
  const [completedVersion, setCompletedVersion] = useState(() => readNote(UPDATED_TO));

  useEffect(() => writeNote(UPDATED_TO, null), []);

  useEffect(() => {
    let current = readNote(UPDATING_FROM);
    // An update started elsewhere (another device, or before this page opened) is followed too.
    if (current === null && (update.installing || update.restarting)) {
      noteUpdateStarted(version);
      current = version;
    }
    if (current !== null && version !== current) {
      // Back on another version: the update worked.
      writeNote(UPDATING_FROM, null);
      writeNote(UPDATED_TO, version);
      // The demo's router lives in the page, so it can't survive a reload.
      // oxlint-disable-next-line react/set-state-in-effect -- the version comes from the router's status
      if (import.meta.env.MODE === 'demo') setCompletedVersion(version);
      else reloadPage();
    } else if (current !== null && !update.installing && (update.failed || update.restartPending)) {
      // It failed, or waits for a restart the owner starts: RasBeacon says so itself.
      writeNote(UPDATING_FROM, null);
    }
  }, [version, update.installing, update.restarting, update.failed, update.restartPending]);

  return completedVersion === version ? completedVersion : null;
}
