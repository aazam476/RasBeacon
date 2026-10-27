// Plugins page: the data kept from removed plugins, and deleting it.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PluginsScreen } from '@/screens/plugins/PluginsScreen';
import { fakeDevice, renderScreen } from '../render';

describe('kept data', () => {
  it('lists what each removed plugin left, and deletes it after asking', async () => {
    const device = fakeDevice((router) => {
      router.plugins.kept = [{ id: 'trip-notes', name: 'Trip Notes', bytes: 48_234_496 }];
    });
    const { user } = renderScreen(<PluginsScreen />, device, '/plugins');
    const row = screen.getByRole('button', { name: /Delete the data of Trip Notes/ });
    expect(row.textContent).toContain('48');
    await user.click(row);
    const alert = within(screen.getByRole('dialog', { name: 'Delete the kept data?' }));
    await user.click(alert.getByRole('button', { name: 'Delete' }));
    expect(device.forgetKeptPluginData).toHaveBeenCalledWith('trip-notes');
  });

  it('shows nothing when no data is kept', () => {
    renderScreen(<PluginsScreen />, fakeDevice(), '/plugins');
    expect(screen.queryByText('Kept data')).toBeNull();
  });
});
