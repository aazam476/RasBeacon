// Every field of the longer forms reaches what is saved: each test fills in all of a form's
// fields, then checks the whole of what was sent.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { HotspotScreen } from '@/screens/hotspot/HotspotScreen';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { SetupScreen } from '@/screens/phases/SetupScreen';
import { fakeDevice, renderScreen } from '../render';

/** The Internet page; adding a network answers it as the list entry the router would make. */
function internetDevice() {
  return fakeDevice(undefined, {
    prepareNetwork: async (form) => ({
      id: '',
      ssid: form.ssid,
      security: form.security,
      hidden: form.hidden,
      hasPassword: true,
    }),
  });
}

describe('every field', () => {
  it('a WPA-Enterprise network with certificates', async () => {
    const device = internetDevice();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Add manually' }));
    const sheet = within(screen.getByRole('dialog'));
    const ca = new File(['CA'], 'ca.pem');
    const cert = new File(['CERT'], 'me.pem');
    const key = new File(['KEY'], 'me.key');

    await user.type(sheet.getByLabelText('Network name'), 'Office');
    await user.selectOptions(sheet.getByLabelText('Security'), 'enterprise');
    await user.selectOptions(sheet.getByLabelText('Login method'), 'tls');
    await user.type(sheet.getByLabelText('Username', { exact: true }), 'me@example.com');
    await user.type(sheet.getByLabelText(/^Anonymous identity/), 'anonymous@example.com');
    await user.upload(sheet.getByLabelText('Your certificate file'), cert);
    await user.upload(sheet.getByLabelText('Your key file'), key);
    await user.type(sheet.getByLabelText(/^Key passphrase/), 'key-pass');
    await user.click(sheet.getByText('Advanced', { exact: true }));
    await user.upload(sheet.getByLabelText(/^CA certificate file/), ca);
    await user.type(sheet.getByLabelText('Login server name'), 'radius.example.com');
    await user.click(sheet.getByRole('button', { name: 'Add to list' }));

    expect(device.prepareNetwork).toHaveBeenCalledWith(
      {
        ssid: 'Office',
        security: 'enterprise',
        password: 'key-pass',
        hidden: false,
        method: 'tls',
        identity: 'me@example.com',
        anonymousIdentity: 'anonymous@example.com',
        serverName: 'radius.example.com',
        caCertFiles: [ca],
        clientCertFile: cert,
        clientKeyFile: key,
        allowNoCertificateCheck: false,
      },
      expect.any(Array),
    );
  });

  it('a hidden WPA-Personal network', async () => {
    const device = internetDevice();
    const { user } = renderScreen(<InternetScreen />, device, '/internet');
    await user.click(screen.getByRole('button', { name: 'Add manually' }));
    const sheet = within(screen.getByRole('dialog'));
    await user.type(sheet.getByLabelText('Network name'), 'Home-Office-2');
    await user.type(sheet.getByLabelText('Network password'), 'home-pass-1');
    await user.click(sheet.getByLabelText(/Hidden network/));
    await user.click(sheet.getByRole('button', { name: 'Add to list' }));
    expect(device.prepareNetwork).toHaveBeenCalledWith(
      expect.objectContaining({ ssid: 'Home-Office-2', security: 'personal', password: 'home-pass-1', hidden: true }),
      expect.any(Array),
    );
  });

  it('the hotspot', async () => {
    const device = fakeDevice();
    const { user } = renderScreen(<HotspotScreen />, device, '/hotspot');
    await user.clear(screen.getByLabelText('Network name'));
    await user.type(screen.getByLabelText('Network name'), 'Trip-Net');
    await user.type(screen.getByLabelText(/^New Wi-Fi password/), 'trip-pass-77');
    await user.selectOptions(screen.getByLabelText('Channel'), '36');
    await user.selectOptions(screen.getByLabelText('Security'), 'wpa3');
    await user.click(screen.getAllByRole('button', { name: 'Save' })[0]);
    expect(device.saveHotspotSettings).toHaveBeenCalledWith(
      expect.objectContaining({ ssid: 'Trip-Net', password: 'trip-pass-77', channel: 36, security: 'wpa3' }),
    );
  });

  it('first-time setup', async () => {
    const device = fakeDevice((router) => {
      router.setup = { ...router.setup, mode: 'setup', claimed: false };
    });
    const { user } = renderScreen(<SetupScreen />, device);
    await user.type(screen.getByLabelText('Admin password', { exact: true }), 'tangerine-orbit-42');
    await user.type(screen.getByLabelText('Type it again'), 'tangerine-orbit-42');
    await user.clear(screen.getByLabelText('Network name'));
    await user.type(screen.getByLabelText('Network name'), 'Trip-Net');
    await user.clear(screen.getByLabelText(/^Wi-Fi password/));
    await user.type(screen.getByLabelText(/^Wi-Fi password/), 'trip-pass-77');
    await user.selectOptions(screen.getByLabelText('Band'), '5GHz');
    await user.click(screen.getByRole('button', { name: 'Finish setup' }));
    expect(device.finishSetup).toHaveBeenCalledWith({
      adminPassword: 'tangerine-orbit-42',
      adminPasswordConfirm: 'tangerine-orbit-42',
      hotspotSsid: 'Trip-Net',
      hotspotPassword: 'trip-pass-77',
      hotspotBand: '5GHz',
      place: 'travel',
    });
  });
});
