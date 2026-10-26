import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type PointerEvent,
  type ReactNode,
} from 'react';
import { currentY, prefersReducedMotion, rubberband, setY, springY } from '@/lib/spring';
import { DESKTOP_QUERY, useMediaQuery } from '@/lib/useMediaQuery';
import { ErrorToastContext } from './errorToastContext';
import { Icon } from './Icon';
import { IconButton } from './IconButton';

/** How long a message stays on screen, not counting time under the pointer or in a hidden tab. */
const SHOW_FOR_MS = 10_000;
/** A swipe this far (px), or this fast (px/ms), sends the message away. */
const SWIPE_DISTANCE = 40;
const SWIPE_VELOCITY = 0.11;
const EASE_OUT = 'cubic-bezier(0.23, 1, 0.32, 1)';

interface ToastMessage {
  message: string;
  id: number;
}

/**
 * Says why a one-tap control failed. Switches, option lists and buttons outside forms have no
 * room for a message of their own (forms and sheets show theirs in place), so their failures
 * appear here: dropping in from the top on phones, like a notification, and rising in the
 * bottom corner on computers. A new message replaces the last one. Each goes away after ten
 * seconds, with its close button, or with a swipe back the way it came.
 */
export function ErrorToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const showError = useCallback(
    (message: string) => setToast((current) => ({ message, id: (current?.id ?? 0) + 1 })),
    [],
  );
  const remove = useCallback((id: number) => setToast((current) => (current?.id === id ? null : current)), []);

  return (
    <ErrorToastContext value={showError}>
      {children}
      {/* Always present, so screen readers announce each message that appears in it. */}
      <div
        role="alert"
        className="pointer-events-none fixed inset-x-0 top-[calc(env(safe-area-inset-top)+0.5rem)] z-50 flex justify-center px-3 lg:top-auto lg:right-6 lg:bottom-6 lg:left-auto lg:px-0"
      >
        {/* The key makes each new message a new Toast, with its own full ten seconds. */}
        {toast && <Toast key={toast.id} toast={toast} onGone={remove} />}
      </div>
    </ErrorToastContext>
  );
}

function useDocumentHidden(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      document.addEventListener('visibilitychange', onChange);
      return () => document.removeEventListener('visibilitychange', onChange);
    },
    () => document.hidden,
  );
}

function Toast({ toast, onGone }: { toast: ToastMessage; onGone: (id: number) => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const desktop = useMediaQuery(DESKTOP_QUERY);
  // Phones: it comes from the top (-1); computers: from the bottom (1). It leaves the same way.
  const edge = desktop ? 1 : -1;
  const leaving = useRef(false);
  const drag = useRef<{ pointerId: number; startY: number; startTime: number; offset: number } | null>(null);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const hidden = useDocumentHidden();
  const remaining = useRef(SHOW_FOR_MS);
  const { id } = toast;

  const enter = useCallback(() => {
    const element = ref.current;
    if (!element) return;
    for (const animation of element.getAnimations()) animation.cancel();
    setY(element, 0);
    const reduced = prefersReducedMotion();
    element.animate(
      reduced
        ? [{ opacity: 0 }, { opacity: 1 }]
        : [
            { opacity: 0, transform: `translate3d(0, ${edge * 100}%, 0) scale(0.96)` },
            { opacity: 1, transform: 'translate3d(0, 0, 0)' },
          ],
      { duration: reduced ? 200 : 400, easing: EASE_OUT },
    );
  }, [edge]);

  /** Back the way it came, carrying the speed of a swipe, then gone. */
  const leave = useCallback(
    (velocity = 0) => {
      const element = ref.current;
      if (!element || leaving.current) return;
      leaving.current = true;
      const reduced = prefersReducedMotion();
      const out = reduced
        ? element.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 150, fill: 'forwards' }).finished
        : Promise.all([
            springY(element, edge * (element.offsetHeight + 32), { response: 0.3, velocity }),
            element.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 200, easing: EASE_OUT, fill: 'forwards' })
              .finished,
          ]);
      out.then(
        () => leaving.current && onGone(id),
        () => {},
      );
    },
    [edge, id, onGone],
  );

  useLayoutEffect(() => {
    enter();
  }, [enter]);

  // The countdown stops while the message is pointed at or focused, and while the tab is hidden.
  const paused = hovered || focused || hidden;
  useEffect(() => {
    if (paused) return;
    const started = Date.now();
    const timer = setTimeout(() => leave(), remaining.current);
    return () => {
      clearTimeout(timer);
      remaining.current = Math.max(2000, remaining.current - (Date.now() - started));
    };
  }, [paused, id, leave]);

  function onPointerDown(event: PointerEvent<HTMLDivElement>) {
    const element = ref.current;
    // Swiped by finger or pen; with a mouse the text can be selected instead.
    if (!element || drag.current || !event.isPrimary || event.pointerType === 'mouse') return;
    if ((event.target as Element).closest('button')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    leaving.current = false;
    const offset = currentY(element);
    for (const animation of element.getAnimations()) animation.cancel();
    setY(element, offset);
    drag.current = { pointerId: event.pointerId, startY: event.clientY, startTime: event.timeStamp, offset };
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const current = drag.current;
    const element = ref.current;
    if (!current || !element || event.pointerId !== current.pointerId) return;
    const wanted = current.offset + event.clientY - current.startY;
    // Toward its edge it follows the finger; the other way it resists.
    setY(element, wanted * edge >= 0 ? wanted : rubberband(wanted, element.offsetHeight));
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    const current = drag.current;
    const element = ref.current;
    if (!current || !element || event.pointerId !== current.pointerId) return;
    drag.current = null;
    const moved = (event.clientY - current.startY) * edge;
    const speed = moved / Math.max(1, event.timeStamp - current.startTime);
    if (moved >= SWIPE_DISTANCE || speed > SWIPE_VELOCITY) leave(speed * 1000 * edge);
    else springY(element, 0, { dampingRatio: 0.85, response: 0.3 });
  }

  return (
    <div
      ref={ref}
      onPointerEnter={(event) => event.pointerType === 'mouse' && setHovered(true)}
      onPointerLeave={() => setHovered(false)}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      className="glass pointer-events-auto flex w-full max-w-md touch-pan-x items-start gap-2.5 rounded-[20px] py-2 pr-2 pl-3.5 select-none lg:w-[22rem] lg:rounded-[14px] lg:select-text"
    >
      <Icon name="error" filled size={22} className="mt-[0.4375rem] text-red lg:mt-1.5 lg:size-5" />
      <p className="min-w-0 flex-1 py-[0.4375rem] text-subhead wrap-break-word text-label lg:py-1.5">{toast.message}</p>
      <IconButton icon="close" ariaLabel="Close message" onClick={() => leave()} />
    </div>
  );
}
