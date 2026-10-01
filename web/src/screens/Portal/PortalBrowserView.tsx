// Markup for the zero-leak login: pictures of the login page in RasBeacon's
// own browser, and controls for typing and moving around.

import type { PortalBrowserLogic } from "./usePortalBrowser";

export type PortalBrowserViewProps = PortalBrowserLogic;

export function PortalBrowserView(props: PortalBrowserViewProps) {
  const { frame } = props;
  return (
    <section aria-label="Login page in RasBeacon's browser">
      <p>
        <strong>{frame?.title || "Opening the login page…"}</strong>
        {frame?.url && (
          <>
            <br />
            <small>{frame.url}</small>
          </>
        )}
      </p>
      {props.error && <p role="alert">{props.error}</p>}

      {props.imageSrc ? (
        <img
          src={props.imageSrc}
          alt="The network's login page. Tap it as you would the real page."
          width={frame?.width}
          height={frame?.height}
          style={{ width: "100%", height: "auto", touchAction: "none", border: "1px solid" }}
          draggable={false}
          onPointerDown={(e) => props.pointerDown({ clientX: e.clientX, clientY: e.clientY })}
          onPointerUp={(e) =>
            props.pointerUp(
              { clientX: e.clientX, clientY: e.clientY },
              e.currentTarget.getBoundingClientRect(),
            )
          }
          onWheel={(e) =>
            props.wheel(
              { clientX: e.clientX, clientY: e.clientY },
              e.currentTarget.getBoundingClientRect(),
              e.deltaY,
            )
          }
        />
      ) : (
        <p>Loading…</p>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          props.sendText();
        }}
      >
        <label>
          Tap a field on the page, then type here
          <input
            type="text"
            autoComplete="off"
            autoCapitalize="off"
            value={props.text}
            onChange={(e) => props.setText(e.target.value)}
          />
        </label>
        <button type="submit">Type it</button>
      </form>
      <button type="button" onClick={() => props.pressKey("Enter")}>
        Enter
      </button>
      <button type="button" onClick={() => props.pressKey("Backspace")}>
        Delete
      </button>
      <button type="button" onClick={() => props.pressKey("Tab")}>
        Next field
      </button>
      <button type="button" onClick={props.back}>
        Back
      </button>
      <button type="button" onClick={props.reload}>
        Reload
      </button>
      <button type="button" onClick={props.close}>
        Close
      </button>
      <p>
        <small>
          This page runs on RasBeacon, not on this phone: swipe to scroll, tap to click. Your
          devices stay offline (inside the VPN) until the login works; then this closes by itself.
        </small>
      </p>
    </section>
  );
}
