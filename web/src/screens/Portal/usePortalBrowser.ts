// Logic for the zero-leak login: the network's login page runs in
// RasBeacon's own browser, and this screen shows pictures of it. Taps,
// swipes and typing are sent back to that browser. Your devices' traffic
// stays in the VPN the whole time.

import { useRef, useState } from "react";
import {
  getPortalBrowserFrame,
  sendPortalBrowserInput,
  startPortalBrowser,
  stopPortalBrowser,
} from "../../api/client";
import type { PortalBrowserFrame, PortalBrowserInput } from "../../api/types";
import { useLongPoll } from "../../shared/useLongPoll";
import { classifyGesture, pageSizeFor, type PictureBox, type PointerSpot } from "./browserGestures";

export interface PortalBrowserLogic {
  /** The latest picture, or undefined until the first one arrives. */
  frame: PortalBrowserFrame | undefined;
  /** The picture as an <img> source, or "" before the first one. */
  imageSrc: string;
  /** Opens RasBeacon's browser on the login page. */
  open: () => void;
  /** Closes it (it also closes by itself once the login worked). */
  close: () => void;
  /** A finger or mouse went down / up on the picture. */
  pointerDown: (spot: PointerSpot) => void;
  pointerUp: (spot: PointerSpot, box: PictureBox) => void;
  /** Mouse wheel over the picture (desktop computers). */
  wheel: (spot: PointerSpot, box: PictureBox, deltaY: number) => void;
  /** The text box under the picture: typed text is sent with sendText. */
  text: string;
  setText: (text: string) => void;
  sendText: () => void;
  pressKey: (key: "Enter" | "Backspace" | "Tab") => void;
  back: () => void;
  reload: () => void;
  busy: boolean;
  error: string | null;
}

function nextFrame(previous: PortalBrowserFrame | undefined): Promise<PortalBrowserFrame> {
  return getPortalBrowserFrame(previous?.seq ?? 0);
}

function isClosed(frame: PortalBrowserFrame): boolean {
  return !frame.running;
}

/**
 * `browsing` is true while the device says its browser is open; `changed`
 * is called after opening or closing so the notice reloads its status.
 */
export function usePortalBrowser(browsing: boolean, changed: () => void): PortalBrowserLogic {
  const frames = useLongPoll(browsing, nextFrame, isClosed);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const downAt = useRef<PointerSpot | null>(null);

  const frame = frames.data?.running ? frames.data : undefined;

  async function run(action: () => Promise<unknown>) {
    setError(null);
    try {
      await action();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  function send(input: PortalBrowserInput) {
    void run(() => sendPortalBrowserInput(input));
  }

  async function open() {
    setBusy(true);
    await run(() => startPortalBrowser(pageSizeFor(window.innerWidth, window.innerHeight)));
    setBusy(false);
    changed();
  }

  async function close() {
    await run(stopPortalBrowser);
    changed();
  }

  return {
    frame,
    imageSrc: frame?.image ? `data:image/jpeg;base64,${frame.image}` : "",
    open: () => void open(),
    close: () => void close(),
    pointerDown: (spot) => {
      downAt.current = spot;
    },
    pointerUp: (spot, box) => {
      const start = downAt.current;
      downAt.current = null;
      if (!start || !frame) return;
      const gesture = classifyGesture(start, spot, box, frame);
      send(
        gesture.kind === "click"
          ? { action: "click", x: gesture.x, y: gesture.y }
          : { action: "scroll", x: gesture.x, y: gesture.y, deltaY: gesture.deltaY },
      );
    },
    wheel: (spot, box, deltaY) => {
      if (!frame) return;
      const { x, y } = classifyGesture(spot, spot, box, frame);
      send({ action: "scroll", x, y, deltaY });
    },
    text,
    setText,
    sendText: () => {
      if (text === "") return;
      send({ action: "type", text });
      setText("");
    },
    pressKey: (key) => send({ action: "key", key }),
    back: () => send({ action: "back" }),
    reload: () => send({ action: "reload" }),
    busy,
    error: error ?? frames.error,
  };
}
