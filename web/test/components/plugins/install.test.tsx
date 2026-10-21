// Plugins page: installing a plugin from a file.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { createMockPlugins, definitionOf, previewOf } from '@/data/demo/plugins';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { fakeDevice, renderScreen } from '../render';

/** What RasBeacon answers after checking a file: the demo's Trip Notes plugin. */
function preview() {
  const state = createMockPlugins();
  const definition = definitionOf('trip-notes')!;
  return previewOf(definition, state, 'staging-1');
}

describe('installing from a file', () => {
  it('checks the file, says what the plugin may do, then installs with the choices', async () => {
    const answer = preview();
    const device = fakeDevice(undefined, { previewPluginFile: async () => answer });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'trip-notes.rba'));
    const sheet = within(await screen.findByRole('dialog', { name: `Install ${answer.name}` }));
    expect(sheet.getByText('It will be able to')).toBeTruthy();

    await user.click(sheet.getByRole('button', { name: 'Install' }));
    expect(device.installPlugin).toHaveBeenCalledWith(
      'staging-1',
      expect.objectContaining({ ports: expect.any(Object) }),
      false,
    );
  });

  it('forgets the checked file when the sheet is cancelled', async () => {
    const device = fakeDevice(undefined, { previewPluginFile: async () => preview() });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'trip-notes.rba'));
    const sheet = within(await screen.findByRole('dialog'));
    await user.click(sheet.getByRole('button', { name: 'Cancel' }));
    expect(device.cancelPluginInstall).toHaveBeenCalledWith('staging-1');
  });
});

// Data kept from an earlier install with the same ID, not known to be this plugin's: the sheet
// asks, off by default, and names the earlier plugin and who signed it.
describe('data kept from an earlier install', () => {
  it.each([
    [
      { name: 'Trip Notes', keyId: 'E4C1 0A77 93BD 2F05' },
      "This one isn't signed, so it may be another author's plugin.",
    ],
    [{ name: 'Trip Notes' }, "That one wasn't signed, so nothing shows this is the same plugin."],
    [{}, "It was kept by RasBeacon 0.1, which didn't note whose it was."],
  ])('asks before using it (%o)', async (keptData, why) => {
    const answer = { ...preview(), keptData };
    const device = fakeDevice(undefined, { previewPluginFile: async () => answer });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'trip-notes.rba'));
    const sheet = within(await screen.findByRole('dialog'));
    const label = keptData.name
      ? 'Use the data kept from the earlier Trip Notes install'
      : 'Use the data kept from an earlier install';
    const toggle = sheet.getByRole('checkbox', { name: label });
    expect(toggle).toHaveProperty('checked', false);
    expect(sheet.getByText(new RegExp(why.replace(/[.?]/g, '\\$&')))).toBeTruthy();
    if (keptData.keyId) expect(sheet.getByText(keptData.keyId)).toBeTruthy();

    // Off: installed without it.
    await user.click(sheet.getByRole('button', { name: 'Install' }));
    expect(device.installPlugin).toHaveBeenCalledWith('staging-1', expect.any(Object), false);
  });

  it('installs with the kept data when the owner turns it on', async () => {
    const answer = {
      ...preview(),
      signed: true,
      keyId: '7F3A 9C2E 4B1D 7162',
      keptData: { name: 'Trip Notes', keyId: 'E4C1 0A77 93BD 2F05' },
    };
    const device = fakeDevice(undefined, { previewPluginFile: async () => answer });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'trip-notes.rba'));
    const sheet = within(await screen.findByRole('dialog'));
    expect(sheet.getByText(/signed with another key/)).toBeTruthy();
    await user.click(sheet.getByRole('checkbox', { name: /Use the data kept/ }));
    await user.click(sheet.getByRole('button', { name: 'Install' }));
    expect(device.installPlugin).toHaveBeenCalledWith('staging-1', expect.any(Object), true);
  });

  it('asks nothing when there is no kept data', async () => {
    const device = fakeDevice(undefined, { previewPluginFile: async () => preview() });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    await user.upload(screen.getByLabelText('Install from a file'), new File(['x'], 'trip-notes.rba'));
    const sheet = within(await screen.findByRole('dialog'));
    expect(sheet.queryByText('Kept data')).toBeNull();
  });
});
