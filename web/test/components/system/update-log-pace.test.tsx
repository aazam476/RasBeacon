// System page: the update log keeps moving. A progress bar shows the megabytes too, and while
// RasBeacon restarts (and can't add lines itself) the page adds one every 5 seconds.
import { act, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UpdateStatus } from '@/data/types';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

/** The System page with these update details. */
function renderSystem(update: Partial<UpdateStatus>) {
  const device = fakeDevice((router) => {
    router.status.update = { ...router.status.update, canInstall: true, ...update };
  });
  return renderScreen(<SystemScreen />, device);
}

/** The log box's text, line by line. */
function logLines(): string[] {
  return [...screen.getByLabelText('Update log').children].map((line) => line.textContent ?? '');
}

describe('the update log keeps moving', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('shows the megabytes after a progress bar', () => {
    renderSystem({
      installing: true,
      log: [{ text: 'Uploading and installing update', percent: 45, detail: '182 of 404 MB' }],
    });
    expect(logLines()).toEqual(['Uploading and installing update 45% [XXXXXXXXX...........] 182 of 404 MB']);
  });

  it('adds a line every 5 seconds while RasBeacon restarts, and says when it takes long', () => {
    const restarting = { text: 'Restarting to finish the install (RasBeacon is offline for about 2 minutes)' };
    renderSystem({ restarting: true, log: [restarting] });
    expect(logLines()).toEqual([restarting.text]);

    act(() => vi.advanceTimersByTime(10_000));
    expect(logLines().slice(1)).toEqual(['Still restarting (5 s so far)', 'Still restarting (10 s so far)']);

    act(() => vi.advanceTimersByTime(170_000));
    const last = logLines().at(-1);
    expect(last).toContain('Still restarting (3 min 0 s so far). This is taking longer than usual');
  });
});
