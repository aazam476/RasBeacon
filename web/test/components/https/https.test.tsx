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

  it('says where to continue after turning on, and offers the HTTPS address', async () => {
    const device = fakeDevice((router) => {
      router.httpsSettings = { ...router.httpsSettings, enabled: false };
    });
    const { user } = renderScreen(<HttpsScreen />, device);
    await user.click(screen.getByRole('switch', { name: /Use HTTPS for these settings pages/ }));
    expect(screen.getByText(/^HTTPS is on. In a few seconds, continue at https:\/\//)).toBeTruthy();
  });

  it('on plain HTTP with HTTPS on, links to the HTTPS address', () => {
    renderScreen(<HttpsScreen />, fakeDevice());
    expect(screen.getByText('You are on plain HTTP.')).toBeTruthy();
    expect(screen.getByRole('link', { name: /^Continue at https:\/\// })).toBeTruthy();
  });

  it('shows the fingerprint, and who made the certificate', () => {
    const device = fakeDevice((router) => {
      router.httpsSettings = { ...router.httpsSettings, fingerprint: 'AB:CD:EF', uploaded: true };
    });
    renderScreen(<HttpsScreen />, device);
    expect(screen.getByText('SHA-256 fingerprint').nextElementSibling?.textContent).toBe('AB:CD:EF');
    expect(screen.getByText('You (uploaded)')).toBeTruthy();
  });

  it('says there is no certificate until HTTPS is first turned on', () => {
    const device = fakeDevice((router) => {
      router.httpsSettings = { ...router.httpsSettings, enabled: false, fingerprint: '' };
    });
    renderScreen(<HttpsScreen />, device);
    expect(screen.getByText('None yet: one is made when you turn HTTPS on.')).toBeTruthy();
  });

  it('uploads your own certificate once both files are chosen', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HttpsScreen />, device);
    const upload = screen.getByRole('button', { name: 'Upload' });
    const certificate = new File(['CERT'], 'cert.pem');
    const privateKey = new File(['KEY'], 'key.pem');
    await user.upload(screen.getByLabelText(/Certificate file/), certificate);
    expect(upload).toHaveProperty('disabled', true);
    await user.upload(screen.getByLabelText(/Private key file/), privateKey);
    await user.click(screen.getByRole('button', { name: 'Upload' }));
    expect(device.uploadCertificate).toHaveBeenCalledWith({ certificate, privateKey });
    expect(await screen.findByText('Certificate uploaded.')).toBeTruthy();
  });
});
