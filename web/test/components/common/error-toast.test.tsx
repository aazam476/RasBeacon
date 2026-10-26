// The error message for one-tap controls (switches, option lists, buttons outside forms): it
// appears on failure, and goes away with its close button, a swipe, or after ten seconds.
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ActionButton, ErrorToastProvider } from '@/ui';
import { finger } from '../render';

/** A button whose action fails with `message`, inside the provider. */
function renderFailingButton(message = 'The VPN could not start.') {
  render(
    <ErrorToastProvider>
      <ActionButton
        action={async () => {
          throw new Error(message);
        }}
      >
        Turn on
      </ActionButton>
    </ErrorToastProvider>,
  );
}

const toast = () => screen.getByRole('alert');

describe('error message', () => {
  afterEach(() => vi.useRealTimers());

  it('appears when the action fails, and closes with its button', async () => {
    const user = userEvent.setup();
    renderFailingButton();
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    expect(toast().textContent).toBe('The VPN could not start.');
    await user.click(screen.getByRole('button', { name: 'Close message' }));
    await waitFor(() => expect(toast().textContent).toBe(''));
  });

  it('a new failure replaces the last message', async () => {
    const user = userEvent.setup();
    renderFailingButton('Second try failed.');
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    expect(screen.getAllByText('Second try failed.')).toHaveLength(1);
  });

  it('goes away by itself after ten seconds, but not while pointed at', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderFailingButton();
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    const message = screen.getByText('The VPN could not start.').parentElement!;
    fireEvent.pointerEnter(message, { pointerType: 'mouse' });
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(toast().textContent).toBe('The VPN could not start.');
    fireEvent.pointerLeave(message);
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    await waitFor(() => expect(toast().textContent).toBe(''));
  });

  it('a message that replaces another gets its own ten seconds', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderFailingButton();
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    // Nine seconds in, a new failure replaces the message.
    await act(() => vi.advanceTimersByTimeAsync(9_000));
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    // Five seconds later it is still there (it used to get only what was left of the first one's
    // time, at least two seconds).
    await act(() => vi.advanceTimersByTimeAsync(5_000));
    expect(toast().textContent).toBe('The VPN could not start.');
    // Well past its ten seconds, so the fake clock also covers the exit animation (with only a
    // second to spare, the animation had to finish in real time, which failed on a busy machine).
    await act(() => vi.advanceTimersByTimeAsync(11_000));
    await waitFor(() => expect(toast().textContent).toBe(''));
  });

  it('a swipe back toward its edge sends it away; a slow, short one doesn’t', async () => {
    const user = userEvent.setup();
    renderFailingButton();
    await user.click(screen.getByRole('button', { name: 'Turn on' }));
    const message = screen.getByText('The VPN could not start.').parentElement!;
    // On phones it comes from the top, so a swipe up sends it away.
    const swipe = (to: number, ms: number) => {
      finger(message, 'pointerDown', 100, 0);
      finger(message, 'pointerMove', to, ms);
      finger(message, 'pointerUp', to, ms);
    };
    // A slow, short one: it goes back into place and stays.
    swipe(90, 1000);
    await act(() => new Promise((resolve) => setTimeout(resolve, 50)));
    expect(toast().textContent).toBe('The VPN could not start.');
    swipe(20, 100);
    await waitFor(() => expect(toast().textContent).toBe(''));
  });
});
