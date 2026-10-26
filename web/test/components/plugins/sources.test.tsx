// Browse plugins: the catalog's sources.
import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
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

  it('loads the catalog again after a source is removed, without asking GitHub', async () => {
    const pluginCatalog = vi.fn(async () => ({ entries: [] }));
    const device = fakeDevice(undefined, { pluginCatalog });
    const builtIn = device.plugins.data!.sources.find((s) => s.builtIn)!;
    const { user } = renderScreen(<CatalogScreen />, device, '/plugins/catalog');
    await user.click(await screen.findByRole('button', { name: `Remove ${builtIn.source}` }));
    await vi.waitFor(() => expect(pluginCatalog).toHaveBeenCalledTimes(2));
    expect(pluginCatalog).toHaveBeenLastCalledWith(false);
  });

  it('asks GitHub after a source is added, so its plugins appear', async () => {
    const pluginCatalog = vi.fn(async () => ({ entries: [] }));
    const device = fakeDevice(undefined, { pluginCatalog });
    const { user } = renderScreen(<CatalogScreen />, device, '/plugins/catalog');
    await user.type(await screen.findByLabelText('Add a source'), 'someone/their-plugins');
    await user.click(screen.getByRole('button', { name: 'Add' }));
    await vi.waitFor(() => expect(pluginCatalog).toHaveBeenLastCalledWith(true));
  });
});
