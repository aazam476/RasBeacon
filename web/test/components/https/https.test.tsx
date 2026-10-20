// HTTPS page: turning it on and making a new certificate.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { HttpsScreen } from '@/screens/https/HttpsScreen';
import { fakeDevice, renderScreen } from '../render';

describe('HTTPS', () => {
  it('turns on with the switch', async () => {
    const device = fakeDevice((router) => {
      router.httpsSettings = { ...router.httpsSettings, enabled: false };
    });
    const { user } = renderScreen(<HttpsScreen />, device);
    await user.click(screen.getByRole('switch', { name: /Use HTTPS for these settings pages/ }));
    expect(device.setHttpsEnabled).toHaveBeenCalledWith(true);
  });

  it('asks before making a new certificate', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HttpsScreen />, device);
    await user.click(screen.getByRole('button', { name: 'Make a new certificate' }));
    const alert = screen.getByRole('dialog', { name: 'Make a new certificate?' });
    expect(device.makeNewCertificate).not.toHaveBeenCalled();
    await user.click(within(alert).getByRole('button', { name: 'Make new' }));
    expect(device.makeNewCertificate).toHaveBeenCalledOnce();
  });
});
