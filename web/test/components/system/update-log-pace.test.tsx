// System page: the update log. A progress bar shows the megabytes and the time left, and the
// restart line stays the last one while RasBeacon restarts.
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

  it('ends with the restart line: nothing is added while RasBeacon restarts', () => {
    const restarting = { text: 'Restarting to finish the install (this can take up to 2 minutes)' };
    renderSystem({ restarting: true, log: [restarting] });
    act(() => vi.advanceTimersByTime(180_000));
    expect(logLines()).toEqual([restarting.text]);
  });
});
