// Full-page messages for states where there is nothing to configure:
// loading, can't reach the device, or a mode that needs action on the
// device itself (setup window closed, bad setup.txt, no Wi-Fi radio).

import type { Mode } from "../../api/types";

export type NoticeKind =
  "loading" | "unreachable" | Extract<Mode, "setup-closed" | "setup-error" | "no-radio">;

export interface NoticeViewProps {
  kind: NoticeKind;
  /** Extra detail, e.g. the network error. */
  detail?: string;
  retry?: () => void;
}

const MESSAGES: Record<NoticeKind, { title: string; body: string }> = {
  loading: { title: "RasBeacon", body: "Loading…" },
  unreachable: {
    title: "Can't reach RasBeacon",
    body: "Check that this device is connected to the RasBeacon hotspot, then try again.",
  },
  "setup-closed": {
    title: "Setup window closed",
    body: "Nobody finished setup within 30 minutes, so the setup network was turned off. Unplug RasBeacon and plug it back in to reopen it.",
  },
  "setup-error": {
    title: "setup.txt can't be used",
    body: "The setup.txt file on the SD card has a problem, so the setup network stays off for safety. Fix or delete the file (see the README's troubleshooting section) and restart RasBeacon.",
  },
  "no-radio": {
    title: "No Wi-Fi adapter found",
    body: "RasBeacon found no Wi-Fi radio that can run a hotspot. Plug in a supported USB Wi-Fi adapter and restart.",
  },
};

export function NoticeView({ kind, detail, retry }: NoticeViewProps) {
  const message = MESSAGES[kind];
  return (
    <main>
      <h1>{message.title}</h1>
      <p>{message.body}</p>
      {detail && <p>{detail}</p>}
      {retry && (
        <button type="button" onClick={retry}>
          Try again
        </button>
      )}
    </main>
  );
}
