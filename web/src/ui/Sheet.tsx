import { useEffect, useId, useRef, type PointerEvent, type ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { currentY, prefersReducedMotion, project, rubberband, setY, springY } from '@/lib/spring';
import { useAction } from '@/lib/useAction';
import { DESKTOP_QUERY, useMediaQuery } from '@/lib/useMediaQuery';
import { Button } from './Button';
import {
  DIALOG_LAYER_CLASSES,
  fadeScrim,
  focusFirstInvalid,
  popIn,
  popOut,
  playExit,
  setScrim,
  useModalDialog,
} from './dialogBase';
import { FormError } from './FormMessage';

interface SheetProps {
  title: string;
  /** The confirm button, e.g. "Add to list". */
  confirmLabel: string;
  /** What it reads while the action runs, e.g. "Adding…". */
  busyLabel: string;
  /** `danger` turns the confirm button red, for actions that can't be undone. */
  tone?: 'default' | 'danger';
  /** `lg` gives longer forms more room on computers. */
  size?: 'md' | 'lg';
  /**
   * The device action behind the confirm button. While it runs, the sheet can't be closed;
   * when it succeeds the sheet closes, and when it fails it stays and shows why.
   */
  onConfirm: () => Promise<void>;
  /** Grays out the confirm button (and Enter does nothing), e.g. while a required field is empty. */
  confirmDisabled?: boolean;
  onClose: () => void;
  /**
   * The content, usually `Section`s of fields. Pass a function to put the messages of a
   * `FieldErrors` rejection next to the fields they are about.
   */
  children?: ReactNode | ((fieldErrors: Readonly<Record<string, string>>) => ReactNode);
}

interface Drag {
  pointerId: number;
  startY: number;
  startOffset: number;
  /** Recent pointer positions, for the speed at release. */
  samples: { time: number; y: number }[];
}

/** Pointer speed in px/s over the last 100 ms (positive = downwards). */
function releaseVelocity(samples: Drag['samples']): number {
  const last = samples[samples.length - 1];
  const first = samples.find((sample) => last.time - sample.time <= 100) ?? last;
  const elapsed = last.time - first.time;
  return elapsed > 0 ? ((last.y - first.y) / elapsed) * 1000 : 0;
}

/**
 * A form that slides up from the bottom on phones, as iOS sheets do: Cancel, the title and the
 * confirm button sit in its bar, and it can be pulled down or flicked away by the bar. On
 * computers it is a centered panel with the buttons at the bottom, as on macOS.
 *
 * It opens when rendered (`{adding && <Sheet … />}`). The content is a form, so Enter in a
 * field confirms; Escape, Cancel and a press on the dimmed page close it, and focus returns to
 * whatever opened it. Mark the field that should get the focus first with `data-autofocus`.
 */
export function Sheet({
  title,
  confirmLabel,
  busyLabel,
  tone = 'default',
  size = 'md',
  onConfirm,
  confirmDisabled = false,
  onClose,
  children,
}: SheetProps) {
  const desktop = useMediaQuery(DESKTOP_QUERY);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const scrimRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLFormElement>(null);
  const closing = useRef(false);
  const drag = useRef<Drag | null>(null);
  const titleId = useId();
  const confirming = useAction(onConfirm);
  const busy = confirming.pending;

  /**
   * Closes at once and plays the way out on a copy (see `playExit`): sliding down on phones,
   * carrying the speed of a flick, or shrinking away on computers.
   */
  function dismiss(velocity = 0) {
    if (closing.current) return;
    closing.current = true;
    playExit(dialogRef.current, ({ scrim, surface }) => {
      fadeScrim(scrim, false);
      const reduced = prefersReducedMotion();
      if (desktop || reduced) return popOut(surface, reduced);
      return springY(surface, surface.offsetHeight + 24, { dampingRatio: 1, response: 0.32, velocity });
    });
    onClose();
  }

  useModalDialog(dialogRef, {
    busy,
    dismiss,
    onOpen: () => {
      const panel = panelRef.current;
      fadeScrim(scrimRef.current, true, 0);
      if (!panel) return;
      if (desktop || prefersReducedMotion()) {
        popIn(panel, prefersReducedMotion());
      } else {
        setY(panel, panel.offsetHeight);
        springY(panel, 0, { dampingRatio: 1, response: 0.42 });
      }
    },
  });

  // When the software keyboard covers the bottom of the screen (iOS), the sheet rises above it.
  useEffect(() => {
    const viewport = window.visualViewport;
    const panel = panelRef.current;
    if (desktop || !viewport || !panel) return;
    const fit = () => {
      const covered = Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop);
      const keyboard = covered > 80;
      panel.style.bottom = keyboard ? `${covered}px` : '';
      panel.style.maxHeight = keyboard ? `${viewport.height - 8}px` : '';
    };
    viewport.addEventListener('resize', fit);
    viewport.addEventListener('scroll', fit);
    return () => {
      viewport.removeEventListener('resize', fit);
      viewport.removeEventListener('scroll', fit);
    };
  }, [desktop]);

  // --- Pulling the sheet by its bar (phones) -------------------------------------------------

  function onPointerDown(event: PointerEvent<HTMLElement>) {
    const panel = panelRef.current;
    // One finger at a time; the bar's buttons stay buttons.
    if (!panel || desktop || busy || drag.current || !event.isPrimary) return;
    if ((event.target as Element).closest('button')) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    // Catching a sheet on its way in stops it where it is drawn, under the finger.
    const offset = currentY(panel);
    setY(panel, offset);
    setScrim(scrimRef.current, Number(getComputedStyle(scrimRef.current!).opacity));
    drag.current = {
      pointerId: event.pointerId,
      startY: event.clientY,
      startOffset: offset,
      samples: [{ time: event.timeStamp, y: event.clientY }],
    };
  }

  function onPointerMove(event: PointerEvent<HTMLElement>) {
    const current = drag.current;
    const panel = panelRef.current;
    if (!current || !panel || event.pointerId !== current.pointerId) return;
    const height = panel.offsetHeight;
    const wanted = current.startOffset + event.clientY - current.startY;
    // Down it follows the finger 1:1; up past the top it resists more the further it goes.
    const y = wanted >= 0 ? wanted : rubberband(wanted, height);
    setY(panel, y);
    setScrim(scrimRef.current, 1 - Math.min(1, Math.max(0, y / height)));
    current.samples.push({ time: event.timeStamp, y: event.clientY });
    if (current.samples.length > 12) current.samples.shift();
  }

  function onPointerUp(event: PointerEvent<HTMLElement>, cancelled = false) {
    const current = drag.current;
    const panel = panelRef.current;
    if (!current || !panel || event.pointerId !== current.pointerId) return;
    drag.current = null;
    // A finger that stopped before letting go has no speed left.
    current.samples.push({ time: event.timeStamp, y: event.clientY });
    const velocity = cancelled ? 0 : releaseVelocity(current.samples);
    const y = currentY(panel);
    // Where a flick would carry it decides, as with scrolling: past halfway, it goes.
    if (y + project(velocity) > panel.offsetHeight / 2 && velocity > -200) {
      dismiss(velocity);
    } else {
      // Back to the top; a flick up overshoots a little, as thrown things do.
      springY(panel, 0, { dampingRatio: velocity < -300 ? 0.8 : 1, response: 0.3, velocity });
      fadeScrim(scrimRef.current, true);
    }
  }

  // --- Content --------------------------------------------------------------------------------

  async function confirm() {
    if (confirmDisabled) return;
    if (await confirming.run()) dismiss();
  }

  // A refused form shows its first problem: the field's message, or the form's at the top.
  useEffect(() => {
    if (Object.keys(confirming.fieldErrors).length > 0) focusFirstInvalid(panelRef.current);
  }, [confirming.fieldErrors]);

  const content = typeof children === 'function' ? children(confirming.fieldErrors) : children;
  const error = confirming.error && <FormError className="px-4 lg:px-1">{confirming.error}</FormError>;
  // Compact in the phone's bar; macOS's push buttons at the foot of the panel on computers.
  const buttonSize = desktop ? 'md' : 'sm';
  const cancelButton = (
    <Button variant="secondary" size={buttonSize} disabled={busy} onClick={() => dismiss()}>
      Cancel
    </Button>
  );
  const confirmButton = (
    <Button
      type="submit"
      size={buttonSize}
      variant={tone === 'danger' ? 'danger-filled' : 'primary'}
      loading={busy}
      disabled={confirmDisabled && !busy}
    >
      {busy ? busyLabel : confirmLabel}
    </Button>
  );

  return (
    <dialog ref={dialogRef} aria-labelledby={titleId} className={DIALOG_LAYER_CLASSES}>
      <div
        ref={scrimRef}
        data-scrim
        aria-hidden="true"
        className="absolute inset-0 bg-scrim"
        onClick={() => {
          if (!busy) dismiss();
        }}
      />
      <div className={cn('pointer-events-none absolute inset-0', desktop && 'grid place-items-center p-8')}>
        <form
          ref={panelRef}
          data-surface
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            confirm();
          }}
          className={cn(
            'elevated pointer-events-auto flex flex-col bg-bg text-left shadow-raised',
            desktop
              ? cn(
                  'max-h-[min(46rem,calc(100dvh-4rem))] w-full overflow-hidden rounded-[14px]',
                  size === 'lg' ? 'max-w-[36rem]' : 'max-w-[30rem]',
                )
              : cn(
                  'absolute inset-x-0 bottom-0 max-h-[calc(100dvh-env(safe-area-inset-top)-0.625rem)] rounded-t-[14px]',
                  // The surface continues below the screen, so a bounce past the top shows no gap.
                  'after:absolute after:inset-x-0 after:top-full after:h-dvh after:bg-bg',
                ),
          )}
        >
          {desktop ? (
            <h2 id={titleId} className="shrink-0 px-5 pt-4 pb-3 text-headline text-label">
              {title}
            </h2>
          ) : (
            // The bar is the handle: a finger anywhere on it, except on its buttons, pulls the sheet.
            <div
              className="shrink-0 cursor-grab touch-none select-none active:cursor-grabbing"
              onPointerDown={onPointerDown}
              onPointerMove={onPointerMove}
              onPointerUp={(event) => onPointerUp(event)}
              onPointerCancel={(event) => onPointerUp(event, true)}
            >
              <div aria-hidden="true" className="mx-auto mt-[5px] h-[5px] w-9 rounded-full bg-label-3/45" />
              {/* With large text the buttons leave no room beside the title, so it moves under them;
                  and if even the two buttons don't fit side by side, the second wraps below. */}
              <div className="@container px-4 pt-1.5 pb-3">
                <div className="grid grid-cols-[minmax(max-content,1fr)_minmax(0,auto)_minmax(max-content,1fr)] items-center gap-2 @max-[18rem]:flex @max-[18rem]:flex-wrap @max-[18rem]:justify-between">
                  <span className="justify-self-start">{cancelButton}</span>
                  <h2
                    id={titleId}
                    className="text-center text-headline text-balance text-label @max-[18rem]:order-last @max-[18rem]:basis-full"
                  >
                    {title}
                  </h2>
                  <span className="justify-self-end @max-[18rem]:ml-auto">{confirmButton}</span>
                </div>
              </div>
            </div>
          )}

          <div
            className={cn(
              'flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto overscroll-contain',
              desktop ? 'px-5 pt-1 pb-5' : 'px-4 pt-1 pb-[calc(1.5rem+env(safe-area-inset-bottom))]',
            )}
          >
            {!desktop && error}
            {content}
          </div>

          {desktop && (
            <div className="flex shrink-0 flex-col gap-3 border-t border-separator px-5 py-3.5">
              {error}
              <div className="flex flex-wrap justify-end gap-2">
                {cancelButton}
                {confirmButton}
              </div>
            </div>
          )}
        </form>
      </div>
    </dialog>
  );
}
