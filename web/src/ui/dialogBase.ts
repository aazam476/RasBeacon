import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';

const EASE_OUT = 'cubic-bezier(0.23, 1, 0.32, 1)';

/**
 * Fades the dimming layer to fully shown or hidden, from whatever it shows right now (a drag
 * may have lightened it). `from` overrides the starting opacity, for the first appearance.
 */
export function fadeScrim(scrim: HTMLElement | null, show: boolean, from?: number) {
  if (!scrim) return;
  const start = from ?? Number(getComputedStyle(scrim).opacity);
  for (const animation of scrim.getAnimations()) animation.cancel();
  const end = show ? 1 : 0;
  scrim.style.opacity = String(end);
  scrim.animate([{ opacity: start }, { opacity: end }], { duration: show ? 250 : 200, easing: EASE_OUT });
}

/** Sets the dimming layer's opacity directly, e.g. while a sheet follows a finger. */
export function setScrim(scrim: HTMLElement | null, opacity: number) {
  if (!scrim) return;
  for (const animation of scrim.getAnimations()) animation.cancel();
  scrim.style.opacity = String(opacity);
}

/** A centered surface arriving: slightly smaller and transparent, settling in place (modals stay centered). */
export function popIn(panel: HTMLElement, reducedMotion: boolean) {
  panel.animate(
    reducedMotion
      ? [{ opacity: 0 }, { opacity: 1 }]
      : [
          { opacity: 0, transform: 'scale(0.96)' },
          { opacity: 1, transform: 'none' },
        ],
    { duration: reducedMotion ? 200 : 250, easing: EASE_OUT },
  );
}

/** …and leaving the same way, faster than it came. */
export function popOut(panel: HTMLElement, reducedMotion: boolean): Promise<void> {
  const animation = panel.animate(
    reducedMotion
      ? [{ opacity: 1 }, { opacity: 0 }]
      : [
          { opacity: 1, transform: 'none' },
          { opacity: 0, transform: 'scale(0.97)' },
        ],
    { duration: 150, easing: EASE_OUT, fill: 'forwards' },
  );
  return animation.finished.then(
    () => undefined,
    () => undefined,
  );
}

interface ModalDialogOptions {
  /** True while the dialog's action runs: it can't be closed then. */
  busy: boolean;
  /** Plays the way out and then removes the dialog. */
  dismiss: () => void;
  /** Runs before the first paint, to place the surface where its entrance starts. */
  onOpen: () => void;
}

/** Text fields only take the focus at once where there is a mouse: on phones it would pop the keyboard up. */
function shouldFocus(element: HTMLElement): boolean {
  const typing = element.matches('input:not([type=checkbox],[type=radio],[type=file]), textarea, select');
  return !typing || window.matchMedia('(pointer: fine)').matches;
}

/**
 * The mechanics shared by alerts and sheets, on the browser's own modal `<dialog>`: it opens
 * when rendered, keeps focus inside, gives it back to whatever opened it, and turns Escape and
 * Android's back gesture into `dismiss` (refused while busy). The element marked
 * `data-autofocus` gets the focus first.
 */
export function useModalDialog(
  dialogRef: RefObject<HTMLDialogElement | null>,
  { busy, dismiss, onOpen }: ModalDialogOptions,
) {
  // Whatever had focus before the dialog opened gets it back when it goes away.
  const [opener] = useState(() => document.activeElement as HTMLElement | null);
  const latest = useRef({ busy, dismiss, onOpen });
  useEffect(() => {
    latest.current = { busy, dismiss, onOpen };
  });

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) {
      dialog.showModal();
      const first = dialog.querySelector<HTMLElement>('[data-autofocus]');
      if (first && shouldFocus(first)) first.focus({ preventScroll: true });
      latest.current.onOpen();
    }
    return () => {
      // Unless the focus has moved on by itself in the meantime.
      const active = document.activeElement;
      if (!active || active === document.body || dialog?.contains(active)) opener?.focus({ preventScroll: true });
    };
  }, [dialogRef, opener]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    // Escape and the back gesture ask to close: the dialog plays its way out instead of
    // vanishing, and stays while busy.
    const onCancel = (event: Event) => {
      event.preventDefault();
      if (!latest.current.busy) latest.current.dismiss();
    };
    // Browsers let a page refuse a close request only once until the user interacts with it
    // again, and Escape doesn't count, so stop the key itself while busy.
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && latest.current.busy) event.preventDefault();
    };
    dialog.addEventListener('cancel', onCancel);
    dialog.addEventListener('keydown', onKeyDown, true);
    return () => {
      dialog.removeEventListener('cancel', onCancel);
      dialog.removeEventListener('keydown', onKeyDown, true);
    };
  }, [dialogRef]);
}

/**
 * Plays a dialog's way out on a copy of it, so the dialog itself can go at once: the page
 * underneath works again immediately and gets the focus back (input is never locked while
 * something animates), and opening the dialog again meanwhile opens a fresh one. The copy is
 * only drawn: it takes no input and isn't there for screen readers. `animate` moves the copy's
 * scrim and surface (marked `data-scrim` and `data-surface`) and resolves when done.
 */
export function playExit(
  dialog: HTMLDialogElement | null,
  animate: (parts: { scrim: HTMLElement; surface: HTMLElement }) => Promise<unknown>,
) {
  const scrim = dialog?.querySelector<HTMLElement>('[data-scrim]');
  const surface = dialog?.querySelector<HTMLElement>('[data-surface]');
  if (!dialog || !scrim || !surface) return;

  const ghost = dialog.cloneNode(true) as HTMLDialogElement;
  ghost.removeAttribute('open');
  ghost.setAttribute('data-closing', '');
  ghost.setAttribute('aria-hidden', 'true');
  ghost.inert = true;
  // No second copy of the dialog's ids (labels, descriptions) while it fades.
  for (const element of ghost.querySelectorAll('[id]')) element.removeAttribute('id');
  const ghostScrim = ghost.querySelector<HTMLElement>('[data-scrim]')!;
  const ghostSurface = ghost.querySelector<HTMLElement>('[data-surface]')!;
  // Mid-animation positions aren't copied with the elements, so they are carried over.
  ghostScrim.style.opacity = getComputedStyle(scrim).opacity;
  ghostSurface.style.transform = getComputedStyle(surface).transform;
  document.body.append(ghost);

  animate({ scrim: ghostScrim, surface: ghostSurface }).finally(() => ghost.remove());
}

/**
 * After a failed confirm, the first field with an error gets the focus, so its message is read
 * out and it is in view.
 */
export function focusFirstInvalid(container: HTMLElement | null) {
  container?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
}

/** Classes that turn a `<dialog>` into a full-window layer holding a scrim and a surface. */
export const DIALOG_LAYER_CLASSES =
  'fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none overflow-hidden border-0 bg-transparent p-0 text-label';
