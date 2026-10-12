import { useEffect, useId, useRef, useState } from 'react';
import { useDevice } from '@/data/device';
import type { PortalBrowserFrame } from '@/data/types';
import { ActionButton, ActivityIndicator, Button, FormError } from '@/ui';

/**
 * The zero-leak login: a live picture of the hotel's login page, open in RasBeacon's own
 * browser. Taps and swipes on the picture and the text typed below go to that browser, so the
 * login happens on RasBeacon while every device stays inside the VPN.
 */
export function PortalBrowser() {
  const { portalBrowser, closePortalBrowser, portalBack, portalReload } = useDevice();
  const { frame, imageSrc, error } = portalBrowser;

  return (
    <section aria-label="Login page in RasBeacon's browser" className="-mt-2 flex flex-col gap-3">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="text-headline wrap-break-word text-label">{frame?.title || 'Opening the login page…'}</p>
          {frame?.url && <p className="selectable font-mono text-footnote wrap-anywhere text-label-2">{frame.url}</p>}
        </div>
      </div>
      {error && <FormError>{error}</FormError>}

      <LoginPagePicture frame={frame} imageSrc={imageSrc} />
      <TypingDock />

      <div className="grid grid-cols-3 gap-2">
        <Button variant="secondary" icon="arrow_back" onClick={portalBack}>
          Back
        </Button>
        <Button variant="secondary" icon="refresh" onClick={portalReload}>
          Reload
        </Button>
        <ActionButton variant="secondary" icon="close" action={closePortalBrowser}>
          Close
        </ActionButton>
      </div>

      <p className="text-footnote text-label-2">
        This page runs on RasBeacon, not on this device: tap to click and swipe to scroll, as on the real page. Your
        devices stay offline (inside the VPN) until the login works; then this closes by itself.
      </p>
    </section>
  );
}

/** The picture of the page. Pointer positions and the picture's box go to the router as they are. */
function LoginPagePicture({ frame, imageSrc }: { frame: PortalBrowserFrame | undefined; imageSrc: string }) {
  const { portalPointerDown, portalPointerUp, portalWheel } = useDevice();
  const picture = useRef<HTMLImageElement>(null);

  // The mouse wheel scrolls the page in the picture, not this page. React's wheel handler can't
  // stop the page from scrolling, so this one is added directly.
  useEffect(() => {
    const element = picture.current;
    if (!element) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      portalWheel({ clientX: event.clientX, clientY: event.clientY }, element.getBoundingClientRect(), event.deltaY);
    };
    element.addEventListener('wheel', onWheel, { passive: false });
    return () => element.removeEventListener('wheel', onWheel);
  }, [portalWheel, imageSrc]);

  if (!imageSrc) {
    return (
      <div className="grid aspect-[2/3] max-h-[70dvh] w-full place-items-center rounded-group bg-cell">
        <ActivityIndicator label="Loading…" className="text-subhead" />
      </div>
    );
  }

  return (
    <img
      ref={picture}
      src={imageSrc}
      alt="The network's login page. Tap it as you would the real page."
      // The page's size gives the picture its proportions; CSS scales it to the screen's width.
      width={frame?.width}
      height={frame?.height}
      draggable={false}
      className="h-auto w-full cursor-pointer touch-none rounded-group bg-white shadow-[0_0_0_0.5px_var(--c-separator)] select-none [-webkit-touch-callout:none]"
      onPointerDown={(event) => {
        // Keep receiving this pointer, so lifting it outside the picture still ends the swipe.
        event.currentTarget.setPointerCapture(event.pointerId);
        portalPointerDown({ clientX: event.clientX, clientY: event.clientY });
      }}
      onPointerUp={(event) =>
        portalPointerUp({ clientX: event.clientX, clientY: event.clientY }, event.currentTarget.getBoundingClientRect())
      }
      // A long press would open the phone's "save image" menu instead of pressing on the page.
      onContextMenu={(event) => event.preventDefault()}
    />
  );
}

/**
 * The typing box and the keys a picture can't offer, on glass at the bottom of the screen, so
 * they stay in reach while the picture is scrolled.
 */
function TypingDock() {
  const { portalType, portalKey } = useDevice();
  const [text, setText] = useState('');
  const inputId = useId();

  return (
    <div className="glass sticky bottom-[max(env(safe-area-inset-bottom),0.75rem)] z-10 flex flex-col gap-2.5 rounded-[22px] p-3 lg:rounded-[14px]">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          // The text goes to the router in the background; problems show above the picture.
          if (text === '') return;
          portalType(text);
          setText('');
        }}
        className="flex flex-col gap-1.5"
      >
        <label htmlFor={inputId} className="px-1 text-footnote text-label-2">
          Tap a field on the page, then type here
        </label>
        <div className="flex gap-2">
          <input
            id={inputId}
            type="text"
            value={text}
            onChange={(event) => setText(event.target.value)}
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            enterKeyHint="send"
            className="min-w-0 flex-1 rounded-full bg-fill-2 px-4 py-2 text-body text-label outline-none placeholder:text-label-3 focus-visible:outline-2 focus-visible:outline-tint"
          />
          <Button type="submit" icon="keyboard_return">
            Type it
          </Button>
        </div>
      </form>
      {/* Three keys in a row; on the narrowest phones two, and the third across the next line. */}
      <div className="flex flex-wrap gap-2 *:grow *:basis-24">
        <Button variant="secondary" size="sm" onClick={() => portalKey('Enter')}>
          Enter
        </Button>
        <Button variant="secondary" size="sm" onClick={() => portalKey('Backspace')}>
          Delete
        </Button>
        <Button variant="secondary" size="sm" onClick={() => portalKey('Tab')}>
          Next field
        </Button>
      </div>
    </div>
  );
}
