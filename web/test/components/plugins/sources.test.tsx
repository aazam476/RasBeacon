// Browse plugins: the catalog's sources.
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CatalogScreen } from '@/screens/plugins/CatalogScreen';
import { fakeDevice, renderScreen } from '../render';

describe('catalog sources', () => {
  it('lets the built-in source be removed like any other', async () => {
    const device = fakeDevice(undefined, { pluginCatalog: async () => ({ entries: [] }) });
    const builtIn = device.plugins.data!.sources.find((s) => s.builtIn)!;
    const { user } = renderScreen(<CatalogScreen />, device, '/plugins/catalog');
    await user.click(await screen.findByRole('button', { name: `Remove ${builtIn.source}` }));
    expect(device.savePluginSources).toHaveBeenCalledWith(
      device.plugins.data!.sources.filter((s) => s !== builtIn).map((s) => s.source),
    );
  });
});
