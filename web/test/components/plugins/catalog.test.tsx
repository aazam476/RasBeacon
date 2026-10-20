// Browse plugins: what the sources offer, with Get, Update or Open as in an app store.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Pages } from '@/App';
import { createMockPlugins, definitionOf, previewOf } from '@/data/demo/plugins';
import type { PluginCatalog, PluginCatalogEntry } from '@/data/types';
import { fakeDevice, renderScreen, section } from '../render';

const SOURCE = 'example/rasbeacon-plugins';

function entry(change: Partial<PluginCatalogEntry>): PluginCatalogEntry {
  return { source: SOURCE, file: 'x.rba', name: 'X', version: '1.0', size: 2_000_000, runsHere: true, ...change };
}

/** Browse plugins, with the router answering `catalog`. */
function renderCatalog(catalog: PluginCatalog, actions: Parameters<typeof fakeDevice>[1] = {}) {
  const device = fakeDevice(undefined, { pluginCatalog: async () => catalog, ...actions });
  return { device, ...renderScreen(<Pages />, device, '/plugins/catalog') };
}

describe('catalog', () => {
  it('groups the plugins by source and says when it last checked', async () => {
    renderCatalog({
      entries: [entry({ name: 'Trip Notes', description: 'Notes for the road.' })],
      checkedAt: '2026-10-08T14:05:00Z',
    });
    await screen.findByText('Trip Notes');
    const group = within(section(SOURCE));
    expect(group.getByText('Trip Notes')).toBeTruthy();
    expect(group.getByText('Notes for the road.')).toBeTruthy();
    expect(screen.getByText(/^Checked /)).toBeTruthy();
  });

  it('Get downloads the plugin and opens the install sheet', async () => {
    const plugins = createMockPlugins();
    const answer = previewOf(definitionOf('trip-notes')!, plugins, 'staging-2');
    const { device, user } = renderCatalog(
      { entries: [entry({ name: 'Trip Notes', file: 'trip-notes.rba' })] },
      { previewCatalogPlugin: async () => answer },
    );
    await user.click(await screen.findByRole('button', { name: 'Get Trip Notes' }));
    expect(device.previewCatalogPlugin).toHaveBeenCalledWith(SOURCE, 'trip-notes.rba');
    expect(await screen.findByRole('dialog', { name: `Install ${answer.name}` })).toBeTruthy();
  });

  it('offers Update for a newer version, and Open when it is up to date', async () => {
    renderCatalog({
      entries: [
        entry({ name: 'Old', file: 'old.rba', id: 'old', version: '2.0', installedVersion: '1.0' }),
        entry({ name: 'Current', file: 'current.rba', id: 'current', version: '1.0', installedVersion: '1.0' }),
      ],
    });
    expect(await screen.findByRole('button', { name: 'Update Old' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Open' })).toBeTruthy();
  });

  it('can’t get a plugin that doesn’t run here or needs a newer RasBeacon', async () => {
    renderCatalog({
      entries: [
        entry({ name: 'Big', file: 'big.rba', runsHere: false }),
        entry({ name: 'New', file: 'new.rba', needsRasBeacon: '0.3.0' }),
      ],
    });
    expect(await screen.findByText("Doesn't run on this Raspberry Pi")).toBeTruthy();
    expect(screen.getByText('Needs RasBeacon 0.3.0 or newer')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Get Big' })).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Get New' })).toHaveProperty('disabled', true);
  });

  it('shows a source’s problem, and says when nothing was found', async () => {
    renderCatalog({ entries: [], errors: { 'broken/source': 'No release found.' } });
    expect(await screen.findByText('No release found.')).toBeTruthy();
    expect(screen.getByText('No plugins found in the sources.')).toBeTruthy();
  });

  it('Check now asks GitHub again', async () => {
    const { device, user } = renderCatalog({ entries: [] });
    await screen.findByText('No plugins found in the sources.');
    await user.click(screen.getByRole('button', { name: 'Check now' }));
    expect(device.pluginCatalog).toHaveBeenLastCalledWith(true);
  });

  it('says when the catalog can’t be read', async () => {
    renderCatalog(
      { entries: [] },
      {
        pluginCatalog: async () => {
          throw new Error('GitHub did not answer.');
        },
      },
    );
    expect((await screen.findByText('GitHub did not answer.')).closest('[role=alert]')).toBeTruthy();
  });

  it('adds a source', async () => {
    const { device, user } = renderCatalog({ entries: [] });
    await user.type(screen.getByLabelText('Add a source'), ' friend/plugins {Enter}');
    expect(device.savePluginSources).toHaveBeenCalledWith([
      ...device.plugins.data!.sources.map((s) => s.source),
      'friend/plugins',
    ]);
  });
});
