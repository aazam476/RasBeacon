// System page: the update log. While an update installs (and after one failed), a closed row
// opens on what RasBeacon is doing, step by step, with progress bars; the page refreshes it every
// second, and asks before closing the page would cut the upload off.
import { screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { UpdateStatus } from '@/data/types';
import { SystemScreen } from '@/screens/system/SystemScreen';
import { fakeDevice, renderScreen } from '../render';

/** The System page with these update details. */
function renderSystem(update: Partial<UpdateStatus>, actions: Parameters<typeof fakeDevice>[1] = {}) {
  const device = fakeDevice((router) => {
    router.status.update = { ...router.status.update, canInstall: true, ...update };
  }, actions);
  return { device, ...renderScreen(<SystemScreen />, device) };
}

const INSTALLING: UpdateStatus['log'] = [
  { text: 'Receiving the update file' },
  { text: "Checking the update file's signature" },
  { text: 'Signature is valid: this is RasBeacon v0.4.0' },
  { text: 'Uploading and installing update', percent: 45 },
];

describe('the update log', () => {
  it('is a closed row while an update installs, with each step and a progress bar', () => {
    renderSystem({ installing: true, log: INSTALLING });
    const row = screen.getByText('Update logs').closest('details')!;
    expect(row.open).toBe(false);
    const log = within(row).getByLabelText('Update log');
    expect(log.textContent).toContain('Signature is valid: this is RasBeacon v0.4.0');
    expect(log.textContent).toContain('Uploading and installing update 45% [XXXXXXXXX...........]');
  });

  it('stays after a failed install, ending with why', () => {
    renderSystem({
      failed: true,
      message: 'The update was not installed: the upload stopped before the whole file arrived.',
      log: [
        ...INSTALLING!,
        { text: 'Upload stopped before the whole file arrived' },
        { text: 'Discarded the update. The running RasBeacon was not changed.' },
      ],
    });
    const log = screen.getByLabelText('Update log');
    expect(log.textContent).toMatch(/Upload stopped before the whole file arrived\s*Discarded the update/);
  });

  it('is not shown when no install is going on', () => {
    renderSystem({ log: INSTALLING });
    expect(screen.queryByText('Update logs')).toBeNull();
  });

  it('is refreshed every second while an update installs', async () => {
    const { device } = renderSystem({ installing: true, log: INSTALLING });
    await waitFor(() => expect(device.reload).toHaveBeenCalledWith('status'), { timeout: 2500 });
  });

  it('asks before the page closes while the file uploads', async () => {
    const { user } = renderSystem({}, { installUpdate: () => new Promise(() => {}) });
    await user.upload(screen.getByLabelText(/Update file/), new File(['x'], 'rasbeacon-0.4.0-rpi64.rbupdate'));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Install' }));
    expect(await screen.findByText(/closing or reloading it stops the upload/)).toBeTruthy();
    const leaving = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(leaving);
    expect(leaving.defaultPrevented).toBe(true);
  });
});
