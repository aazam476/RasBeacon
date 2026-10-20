// The sheet behind every form that opens over a page: confirming, closing, and on phones
// pulling it down by its bar. jsdom has no layout, so each test gives the sheet a height.
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { FieldErrors } from '@/data/errors';
import { Sheet, TextField } from '@/ui';
import { finger } from '../render';
import { setViewport } from '../setup';

/** A sheet 600 px tall with one field; `onConfirm` and `onClose` are spies. */
function renderSheet(options: { onConfirm?: () => Promise<void>; confirmDisabled?: boolean } = {}) {
  const onConfirm = vi.fn(options.onConfirm ?? (async () => undefined));
  const onClose = vi.fn();
  render(
    <Sheet
      title="Rename profile"
      confirmLabel="Save"
      busyLabel="Saving…"
      onConfirm={onConfirm}
      confirmDisabled={options.confirmDisabled}
      onClose={onClose}
    >
      {(errors) => <TextField label="Name" value="" onChange={() => undefined} error={errors.name} />}
    </Sheet>,
  );
  const sheet = screen.getByRole('dialog', { name: 'Rename profile' });
  const panel = sheet.querySelector('form')!;
  Object.defineProperty(panel, 'offsetHeight', { value: 600 });
  return { sheet, panel, onConfirm, onClose, user: userEvent.setup() };
}

/** The bar a finger pulls (phones): the row with the title. */
const bar = () => screen.getByRole('heading', { name: 'Rename profile' }).parentElement!.parentElement!;

/** A finger on the bar moving from y = 100 to `to` in `ms` milliseconds, then letting go. */
function pull(to: number, ms: number) {
  const target = bar();
  finger(target, 'pointerDown', 100, 0);
  finger(target, 'pointerMove', (100 + to) / 2, ms / 2);
  finger(target, 'pointerMove', to, ms);
  finger(target, 'pointerUp', to, ms);
}

describe('sheet', () => {
  it('confirms, then closes', async () => {
    const { onConfirm, onClose, user } = renderSheet();
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(onConfirm).toHaveBeenCalledOnce();
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  });

  it('stays open with the problem when confirming fails', async () => {
    const { onClose, user, sheet } = renderSheet({
      onConfirm: async () => {
        throw new Error('RasBeacon did not answer.');
      },
    });
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText('RasBeacon did not answer.')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('puts a field’s problem next to it', async () => {
    const { user, sheet } = renderSheet({
      onConfirm: async () => {
        throw new FieldErrors({ name: 'Another profile has this name.' });
      },
    });
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText('Another profile has this name.')).toBeTruthy();
    expect(within(sheet).getByLabelText('Name').getAttribute('aria-invalid')).toBe('true');
  });

  it('a grayed-out Save does nothing, also with Enter', async () => {
    const { onConfirm, user } = renderSheet({ confirmDisabled: true });
    expect(screen.getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
    await user.type(screen.getByLabelText('Name'), '{Enter}');
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it.each(['Cancel', 'Escape', 'the dimmed page'])('%s closes it', async (close) => {
    const { onClose, user, sheet } = renderSheet();
    if (close === 'Cancel') await user.click(within(sheet).getByRole('button', { name: 'Cancel' }));
    if (close === 'Escape') fireEvent(sheet, new Event('cancel', { cancelable: true }));
    if (close === 'the dimmed page') await user.click(sheet.querySelector('[data-scrim]')!);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('can’t be closed while it is saving', async () => {
    let finish = () => undefined as void;
    const { onClose, user, sheet } = renderSheet({ onConfirm: () => new Promise((resolve) => (finish = resolve)) });
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(within(sheet).getByRole('button', { name: 'Cancel' })).toHaveProperty('disabled', true);
    fireEvent.click(sheet.querySelector('[data-scrim]')!);
    expect(onClose).not.toHaveBeenCalled();
    finish();
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  });
});

describe('pulling the sheet on a phone', () => {
  it('a flick down sends it away', () => {
    const { onClose } = renderSheet();
    pull(220, 80);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('pulled past halfway and let go slowly, it goes too', () => {
    const { onClose } = renderSheet();
    pull(500, 2000);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('a short pull springs back', () => {
    const { onClose, panel } = renderSheet();
    pull(160, 2000);
    expect(onClose).not.toHaveBeenCalled();
    expect(panel.style.transform).toBe('translate3d(0, 0px, 0)');
  });

  it('pulled up, it resists and comes back', () => {
    const { onClose, panel } = renderSheet();
    const target = bar();
    finger(target, 'pointerDown', 300, 0);
    finger(target, 'pointerMove', 100, 100);
    // It follows less than the finger moved.
    const followed = -Number(/(-?[\d.]+)px/.exec(panel.style.transform)?.[1]);
    expect(followed).toBeGreaterThan(0);
    expect(followed).toBeLessThan(200);
    finger(target, 'pointerUp', 100, 120);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a cancelled touch (a phone call, say) springs back', () => {
    const { onClose } = renderSheet();
    const target = bar();
    finger(target, 'pointerDown', 100, 0);
    finger(target, 'pointerMove', 200, 50);
    finger(target, 'pointerCancel', 200, 60);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('the bar’s buttons stay buttons, and a second finger is ignored', () => {
    const { onClose } = renderSheet();
    const cancel = screen.getByRole('button', { name: 'Cancel' });
    finger(cancel, 'pointerDown', 100, 0);
    finger(bar(), 'pointerUp', 600, 50);
    finger(bar(), 'pointerDown', 100, 0, { pointerId: 2, isPrimary: false });
    finger(bar(), 'pointerUp', 600, 50, { pointerId: 2 });
    expect(onClose).not.toHaveBeenCalled();
  });

  it('rises above the software keyboard', () => {
    const viewport = Object.assign(new EventTarget(), { height: 400, offsetTop: 0 });
    vi.stubGlobal('visualViewport', viewport);
    const { panel } = renderSheet();
    viewport.dispatchEvent(new Event('resize'));
    expect(panel.style.bottom).toBe(`${window.innerHeight - 400}px`);
    viewport.height = window.innerHeight;
    viewport.dispatchEvent(new Event('resize'));
    expect(panel.style.bottom).toBe('');
  });
});

describe('on a computer', () => {
  it('is a panel with the buttons at the bottom, and can’t be pulled', () => {
    setViewport('desktop');
    const { onClose, sheet } = renderSheet();
    const buttons = within(sheet)
      .getAllByRole('button')
      .map((b) => b.textContent);
    expect(buttons).toEqual(['Cancel', 'Save']);
    const title = screen.getByRole('heading', { name: 'Rename profile' });
    finger(title, 'pointerDown', 100, 0);
    finger(title, 'pointerUp', 600, 50);
    expect(onClose).not.toHaveBeenCalled();
  });
});
