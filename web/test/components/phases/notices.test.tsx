// The full-page notices, such as "RasBeacon is off" after shutting down.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NoticeScreen } from '@/screens/phases/NoticeScreen';
import { fakeDevice, renderScreen } from '../render';

describe('notices', () => {
  it('says how to turn RasBeacon on again after shutting down, and tries again when asked', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<NoticeScreen kind="off" />, device);
    expect(screen.getByRole('heading', { name: 'RasBeacon is off' })).toBeTruthy();
    expect(screen.getByText(/Unplug the power and plug it back in/)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(device.retry).toHaveBeenCalledOnce();
  });

  it('shows the details of a problem', () => {
    renderScreen(<NoticeScreen kind="unreachable" detail="connection refused" />, fakeDevice());
    expect(screen.getByText("Can't reach RasBeacon")).toBeTruthy();
    expect(screen.getByText('Details: connection refused')).toBeTruthy();
  });
});
