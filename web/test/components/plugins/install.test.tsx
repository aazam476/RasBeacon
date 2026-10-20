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
