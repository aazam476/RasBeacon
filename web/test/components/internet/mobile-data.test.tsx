// The data phones and modems carried this month, with a monthly limit that only warns.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MobileData } from '@/data/types';
import { ordinal } from '@/lib/format';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { fakeDevice, renderScreen, section } from '../render';

const october: MobileData = { usedBytes: 4_620_000_000, since: '2026-10-01T00:00:00Z', limitMb: 0, resetDay: 1 };

const withData = (data: Partial<MobileData> = {}) =>
  fakeDevice((router) => {
    router.status.uplink.mobileData = { ...october, ...data };
  });

describe('mobile data', () => {
  it('shows nothing without a phone, a modem or a limit', () => {
    renderScreen(<InternetScreen />, fakeDevice(), '/internet');
    expect(screen.queryByRole('heading', { name: 'Mobile data' })).toBeNull();
  });

  it('shows what was used since the month started', () => {
    renderScreen(<InternetScreen />, withData(), '/internet');
    expect(screen.getByText(/^4\.6\sGB$/)).toBeTruthy();
    expect(screen.getByText(/since October 1\./)).toBeTruthy();
    expect(screen.queryByRole('meter')).toBeNull();
  });

  it('shows the share of the limit, orange near it and red over it', () => {
    const { unmount } = renderScreen(<InternetScreen />, withData({ limitMb: 5000 }), '/internet');
    expect(screen.getByText(/^4\.6\sGB of 5\sGB$/)).toBeTruthy();
    const meter = screen.getByRole('meter', { name: 'Mobile data used this month' });
    expect(meter.getAttribute('aria-valuenow')).toBe('92');
    expect(meter.innerHTML).toContain('bg-orange-fill');
    unmount();
    renderScreen(<InternetScreen />, withData({ limitMb: 4000 }), '/internet');
    expect(screen.getByRole('meter').innerHTML).toContain('bg-red-fill');
  });

  it('saves the limit in MB and the day the month starts', async () => {
    const device = withData();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.type(screen.getByLabelText('Monthly limit'), '2.5');
    await user.selectOptions(screen.getByLabelText('Month starts on the'), '15');
    await user.click(within(section('Mobile data')).getByRole('button', { name: 'Save' }));
    expect(device.saveMobileData).toHaveBeenCalledWith(2500, 15);
  });

  it('checks the amount while it is typed', async () => {
    const device = withData({ limitMb: 5000 });
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    const amount = screen.getByLabelText('Monthly limit');
    await user.clear(amount);
    await user.type(amount, 'lots');
    expect(screen.getByText('Enter an amount up to 1,000 GB, or leave it empty for no limit.')).toBeTruthy();
    expect(within(section('Mobile data')).getByRole('button', { name: 'Save' })).toHaveProperty('disabled', true);
  });

  it('resets the count after asking', async () => {
    const device = withData();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Reset count' }));
    expect(screen.getByText(/starts again from zero/)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Reset' }));
    expect(device.resetMobileData).toHaveBeenCalled();
  });

  it('names the days of the month', () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 28].map(ordinal)).toEqual([
      '1st',
      '2nd',
      '3rd',
      '4th',
      '11th',
      '12th',
      '13th',
      '21st',
      '22nd',
      '23rd',
      '28th',
    ]);
  });
});
