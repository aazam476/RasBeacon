// A plugin's form (ports, settings, USB drive), drawn from its manifest. The install sheet and
// the plugin's page both use it; here it is tested on its own.
import { screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import type { PluginDrive, PluginPort, PluginSetting } from '@/data/types';
import { PluginFields } from '@/screens/plugins/PluginFields';
import { draftOf, type ChoicesDraft } from '@/screens/plugins/pluginDisplay';
import { fakeDevice, renderScreen } from '../render';

const ports: PluginPort[] = [{ key: 'tcp/3000', name: 'Web page', protocol: 'tcp', container: 3000, host: 3000 }];

const settings: PluginSetting[] = [
  { key: 'name', label: 'Share name', type: 'text', unit: 'letters', value: 'Files' },
  { key: 'secret', label: 'Password', type: 'password', value: '', hasValue: true },
  { key: 'size', label: 'Cache', type: 'number', unit: 'MB', min: 1, max: 512, value: '64' },
  { key: 'guest', label: 'Allow guests', type: 'switch', help: 'Without a password.', value: 'false' },
  {
    key: 'mode',
    label: 'Mode',
    type: 'select',
    options: [
      { value: 'fast', label: 'Fast' },
      { value: 'safe', label: 'Safe' },
    ],
    value: 'fast',
  },
];

/** The form with its own state, as the sheet and page hold it; `changes` records each change. */
function renderFields(options: { usesDrive?: boolean; drives?: () => Promise<PluginDrive[]>; drive?: string } = {}) {
  const changes: ChoicesDraft[] = [];
  function Form() {
    const [draft, setDraft] = useState(draftOf(ports, settings, options.drive));
    return (
      <PluginFields
        ports={ports}
        settings={settings}
        usesDrive={options.usesDrive ?? false}
        draft={draft}
        onChange={(next) => {
          changes.push(next);
          setDraft(next);
        }}
        errors={{ 'ports.tcp/3000': 'This port is in use.' }}
        host="rasbeacon.lan"
      />
    );
  }
  const device = fakeDevice(undefined, { pluginDrives: options.drives ?? (async () => []) });
  return { changes, ...renderScreen(<Form />, device) };
}

describe('plugin form', () => {
  it('ports take digits only, and show the router’s error', async () => {
    const { changes, user } = renderFields();
    const port = screen.getByLabelText(/Web page/);
    await user.clear(port);
    await user.type(port, '80a8');
    expect(changes.at(-1)?.ports['tcp/3000']).toBe('808');
    expect(screen.getByText('This port is in use.')).toBeTruthy();
    expect(screen.getByText(/rasbeacon.lan:<port>/)).toBeTruthy();
  });

  it('each kind of setting edits its own value', async () => {
    const { changes, user } = renderFields();
    await user.type(screen.getByLabelText(/Share name \(letters\)/), 's');
    await user.type(screen.getByLabelText(/^Password/), 'pw');
    await user.clear(screen.getByLabelText(/Cache/));
    await user.type(screen.getByLabelText(/Cache/), '128');
    await user.click(screen.getByRole('checkbox', { name: /Allow guests/ }));
    await user.selectOptions(screen.getByLabelText('Mode'), 'safe');
    expect(changes.at(-1)?.settings).toEqual({
      name: 'Filess',
      secret: 'pw',
      size: '128',
      guest: 'true',
      mode: 'safe',
    });
  });

  it('says a saved password stays when left empty, and gives a number its range', () => {
    renderFields();
    expect(screen.getByText('(leave empty to keep it)')).toBeTruthy();
    expect(screen.getByText('(MB, 1 to 512)')).toBeTruthy();
  });

  it('lists the USB drives plugged in', async () => {
    const { changes, user } = renderFields({
      usesDrive: true,
      drives: async () => [{ uuid: 'u1', label: 'Photos', fsType: 'exfat', size: 64e9, usedBy: 'Samba' }],
    });
    const drive = screen.getByLabelText('Drive');
    expect(await screen.findByRole('option', { name: /Photos · .* exfat \(used by Samba\)/ })).toBeTruthy();
    await user.selectOptions(drive, 'u1');
    expect(changes.at(-1)?.drive).toBe('u1');
  });

  it('keeps a chosen drive that is unplugged, and says why the list is empty', async () => {
    renderFields({
      usesDrive: true,
      drive: 'gone',
      drives: async () => {
        throw new Error("Can't list the drives.");
      },
    });
    expect(await screen.findByText("Can't list the drives.")).toBeTruthy();
    expect(screen.getByRole('option', { name: 'The chosen drive (not plugged in)' })).toBeTruthy();
  });
});
