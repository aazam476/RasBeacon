import { useId, useRef, type ReactNode } from 'react';
import { prefersReducedMotion } from '@/lib/spring';
import { useAction } from '@/lib/useAction';
import { Button } from './Button';
import { DIALOG_LAYER_CLASSES, fadeScrim, playExit, popIn, popOut, useModalDialog } from './dialogBase';
import { FormError } from './FormMessage';

interface AlertDialogProps {
  title: string;
  /** What will happen, in a sentence or two. */
  children?: ReactNode;
  /** The confirm button, e.g. "Reboot". */
  confirmLabel: string;
  /** What it reads while the action runs, e.g. "Rebooting…". */
  busyLabel: string;
  /** `danger` for actions that disrupt or can't be undone: the button turns red. */
  tone?: 'default' | 'danger';
  /**
   * The device action behind the confirm button. While it runs, the alert can't be closed;
   * when it succeeds the alert closes, and when it fails it stays and shows why.
   */
  onConfirm: () => Promise<void>;
  onClose: () => void;
}

/**
 * A question before doing something that matters, centered over a dimmed page, with Cancel and
 * the action. It opens when rendered (`{asking && <AlertDialog … />}`). Cancel has the focus
 * at first, so a stray Enter never confirms; Escape and a press on the dimmed page cancel.
 */
export function AlertDialog({
  title,
  children,
  confirmLabel,
  busyLabel,
  tone = 'default',
  onConfirm,
  onClose,
}: AlertDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const scrimRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLFormElement>(null);
  const closing = useRef(false);
  const titleId = useId();
  const messageId = useId();
  const confirming = useAction(onConfirm);
  const busy = confirming.pending;

  /** Closes at once and plays the way out on a copy (see `playExit`). */
  function dismiss() {
    if (closing.current) return;
    closing.current = true;
    playExit(dialogRef.current, ({ scrim, surface }) => {
      fadeScrim(scrim, false);
      return popOut(surface, prefersReducedMotion());
    });
    onClose();
  }

  useModalDialog(dialogRef, {
    busy,
    dismiss,
    onOpen: () => {
      fadeScrim(scrimRef.current, true, 0);
      if (panelRef.current) popIn(panelRef.current, prefersReducedMotion());
    },
  });

  return (
    <dialog ref={dialogRef} aria-labelledby={titleId} aria-describedby={messageId} className={DIALOG_LAYER_CLASSES}>
      <div
        ref={scrimRef}
        data-scrim
        aria-hidden="true"
        className="absolute inset-0 bg-scrim"
        onClick={() => {
          if (!busy) dismiss();
        }}
      />
      <div className="pointer-events-none absolute inset-0 grid place-items-center p-6">
        <form
          ref={panelRef}
          data-surface
          className="pointer-events-auto flex w-full max-w-[19rem] flex-col gap-4 rounded-[22px] bg-raised p-5 text-center shadow-raised lg:max-w-[17rem] lg:rounded-[16px] lg:p-4"
          onSubmit={async (event) => {
            event.preventDefault();
            if (await confirming.run()) dismiss();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <h2 id={titleId} className="text-headline wrap-break-word text-label">
              {title}
            </h2>
            <div id={messageId} className="flex flex-col gap-2 text-subhead wrap-break-word text-label">
              {children}
            </div>
          </div>
          {confirming.error && <FormError className="justify-center text-left">{confirming.error}</FormError>}
          {/* Side by side when both labels fit, stacked otherwise; the action is last. */}
          <div className="flex flex-wrap-reverse gap-2 *:grow *:basis-0 *:min-w-fit">
            <Button variant="secondary" data-autofocus disabled={busy} onClick={dismiss}>
              Cancel
            </Button>
            <Button type="submit" variant={tone === 'danger' ? 'danger-filled' : 'primary'} loading={busy}>
              {busy ? busyLabel : confirmLabel}
            </Button>
          </div>
        </form>
      </div>
    </dialog>
  );
}
