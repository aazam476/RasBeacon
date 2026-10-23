// The hotel login page screen (/#/portal).
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PortalScreen } from '@/screens/portal/PortalScreen';
import { fakeDevice, renderScreen } from '../render';

describe('hotel login page', () => {
  it('says there is nothing to do when no login page is in the way', () => {
    renderScreen(
      <PortalScreen />,
      fakeDevice((router) => {
        router.status.portal = { ...router.status.portal, detected: false };
      }),
      '/portal',
    );
    expect(screen.getByText('Nothing to do')).toBeTruthy();
  });

  it('offers RasBeacon’s own browser first, and login mode as the other way', async () => {
    const device = fakeDevice((router) => {
      router.status.portal = {
        ...router.status.portal,
        detected: true,
        canBrowse: true,
        canStart: true,
        loginMode: false,
        browsing: false,
      };
    });
    const { user } = renderScreen(<PortalScreen />, device, '/portal');
    expect(screen.getByText('Log in through RasBeacon')).toBeTruthy();
    expect(screen.getByText('Recommended')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Check again' }));
    expect(device.checkPortal).toHaveBeenCalledOnce();
  });
});
