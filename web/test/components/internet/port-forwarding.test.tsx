// Internet → Port forwarding: the rules, the notice about whether the internet can reach them,
// and the sheet that adds, changes and deletes one.
import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { MockRouter } from '@/data/demo/mockData';
import type { DeviceActions } from '@/data/device';
import { FieldErrors } from '@/data/errors';
import type { PortForwardRule } from '@/data/types';
import { InternetScreen } from '@/screens/internet/InternetScreen';
import { PortForwardingScreen } from '@/screens/internet/PortForwardingScreen';
import { fakeDevice, renderScreen, type FakeDevice } from '../render';

const PATH = '/internet/port-forwarding';
// In the sample router: the office desktop has a fixed address, the iPhone doesn't.
const DESKTOP = 'a4:83:e7:12:34:56';
const IPHONE = 'bc:d0:74:a1:8f:2c';

/** The sample router as a fake device, changed by `change` (its rules, its uplink). */
function device(change?: (router: MockRouter) => void, actions: Partial<DeviceActions> = {}): FakeDevice {
  return fakeDevice(change, actions);
}

/** Opens the Add rule sheet and fills in a rule to `target` (a menu label). */
async function addRule(user: ReturnType<typeof renderScreen>['user'], target: string, ports = '8080') {
  await user.click(screen.getByRole('button', { name: 'Add rule' }));
  const sheet = screen.getByRole('dialog');
  await user.type(within(sheet).getByRole('textbox', { name: 'Name' }), 'Web');
  await user.type(within(sheet).getByRole('textbox', { name: 'Outside port' }), ports);
  await user.selectOptions(within(sheet).getByRole('combobox', { name: 'Goes to' }), target);
  return sheet;
}

describe('port forwarding page', () => {
  it('lists the rules, with what each forwards and where', () => {
    renderScreen(<PortForwardingScreen />, device(), PATH);
    const minecraft = screen.getByRole('button', { name: 'Minecraft server' });
    expect(minecraft.textContent).toContain('TCP 25565 to desktop-office.lan');
    const steam = screen.getByRole('button', { name: 'Steam games' });
    expect(steam.textContent).toContain('Off');
    expect(steam.textContent).toContain('UDP 27015–27030');
    expect(screen.getAllByText('10.15.82.114').length).toBeGreaterThan(0);
  });

  it.each([
    ['10.15.82.114', 'RasBeacon is behind another router'],
    ['100.64.1.2', 'The internet can’t reach this network'],
  ])('says when the internet can’t reach %s', (address, title) => {
    renderScreen(
      <PortForwardingScreen />,
      device((router) => void (router.status.uplink.address = address)),
      PATH,
    );
    expect(screen.getByText(title)).toBeTruthy();
  });

  it('says nothing more on a public address', () => {
    renderScreen(
      <PortForwardingScreen />,
      device((router) => void (router.status.uplink.address = '203.0.113.7')),
      PATH,
    );
    expect(screen.queryByText(/behind another router|can’t reach/)).toBeNull();
  });

  it('waits while offline', () => {
    renderScreen(
      <PortForwardingScreen />,
      device((router) => void (router.status.uplink.connected = false)),
      PATH,
    );
    expect(screen.getByText('Not connected to the internet')).toBeTruthy();
    expect(screen.queryByText('Outside address')).toBeNull();
  });

  it('warns about a port often attacked', () => {
    renderScreen(
      <PortForwardingScreen />,
      device((router) => void (router.portForwardingRules[0].outsidePort = 3389)),
      PATH,
    );
    expect(screen.getByText(/Minecraft server: Port 3389 \(Remote Desktop\) is often attacked/)).toBeTruthy();
  });

  it('stops adding at 50 rules', () => {
    renderScreen(
      <PortForwardingScreen />,
      device((router) => {
        router.portForwardingRules = Array.from({ length: 50 }, (_, index) => ({
          id: `r${index}`,
          name: `Rule ${index}`,
          protocol: 'tcp' as const,
          outsidePort: 1000 + index,
          mac: DESKTOP,
          enabled: false,
        }));
      }),
      PATH,
    );
    expect(screen.getByRole('button', { name: /Add rule/ })).toHaveProperty('disabled', true);
    expect(screen.getByText('RasBeacon keeps at most 50 rules.')).toBeTruthy();
  });

  it('shows on the Internet page with how many rules are on', () => {
    renderScreen(<InternetScreen />, device(), '/internet');
    const link = screen.getByRole('link', { name: 'Port forwarding' });
    expect(link.getAttribute('href')).toBe(PATH);
    expect(screen.getByText('1 rule')).toBeTruthy();
  });

  it.each([
    [[], 'Off'],
    [[true, true], '2 rules'],
  ])('counts %j as %s on the Internet page', (enabled, value) => {
    const rules: PortForwardRule[] = enabled.map((on, index) => ({
      id: `r${index}`,
      name: `Rule ${index}`,
      protocol: 'tcp',
      outsidePort: 1000 + index,
      mac: DESKTOP,
      enabled: on,
    }));
    renderScreen(
      <InternetScreen />,
      device((router) => void (router.portForwardingRules = rules)),
      '/internet',
    );
    expect(screen.getByText(value)).toBeTruthy();
  });
});

describe('port forwarding sheet', () => {
  it('adds a rule to a device with a fixed address', async () => {
    const fake = device();
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    const sheet = await addRule(user, 'desktop-office.lan');
    await user.type(within(sheet).getByRole('textbox', { name: /Inside port/ }), '80');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(fake.setFixedAddress).not.toHaveBeenCalled();
    const saved = fake.savePortForwarding.mock.calls[0][0];
    expect(saved).toHaveLength(3);
    expect(saved[2]).toEqual({
      id: '',
      name: 'Web',
      protocol: 'tcp',
      outsidePort: 8080,
      outsidePortEnd: undefined,
      insidePort: 80,
      mac: DESKTOP,
      plugin: undefined,
      enabled: true,
    });
  });

  it('gives a device without a fixed address the one it has now, first', async () => {
    const fake = device();
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    const sheet = await addRule(user, 'iphone-15-pro-personal-icloud-private-relay.lan');
    expect(within(sheet).getByText(/Saving also gives this device the fixed address 192\.168\.8\.102/)).toBeTruthy();
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(fake.setFixedAddress).toHaveBeenCalledWith(IPHONE, '192.168.8.102');
    expect(fake.savePortForwarding).toHaveBeenCalled();
  });

  it('shows a refused fixed address under the menu', async () => {
    const fake = device(undefined, {
      setFixedAddress: async () => {
        throw new FieldErrors({ address: 'Another device already uses 192.168.8.102.' });
      },
    });
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    const sheet = await addRule(user, 'iphone-15-pro-personal-icloud-private-relay.lan');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText('Another device already uses 192.168.8.102.')).toBeTruthy();
    expect(fake.savePortForwarding).not.toHaveBeenCalled();
  });

  it('passes on any other failure of the fixed address', async () => {
    const fake = device(undefined, {
      setFixedAddress: async () => {
        throw new Error('RasBeacon didn’t answer.');
      },
    });
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    const sheet = await addRule(user, 'iphone-15-pro-personal-icloud-private-relay.lan');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText('RasBeacon didn’t answer.')).toBeTruthy();
  });

  it('forwards a range to a plugin, without an inside port', async () => {
    const fake = device();
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    const plugin = fake.status.data!.plugins[0];
    const sheet = await addRule(user, `${plugin.name} (plugin)`, '27015-27020');
    expect(within(sheet).queryByRole('textbox', { name: /Inside port/ })).toBeNull();
    await user.selectOptions(within(sheet).getByRole('combobox', { name: 'Protocol' }), 'TCP and UDP');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(fake.savePortForwarding.mock.calls[0][0][2]).toMatchObject({
      protocol: 'both',
      outsidePort: 27015,
      outsidePortEnd: 27020,
      insidePort: undefined,
      plugin: plugin.id,
      mac: undefined,
    });
  });

  it('checks the ports as they are typed', async () => {
    const { user } = renderScreen(<PortForwardingScreen />, device(), PATH);
    const sheet = await addRule(user, 'desktop-office.lan', '25565');
    const save = within(sheet).getByRole('button', { name: 'Save' });
    // Minecraft server already forwards TCP 25565.
    expect(within(sheet).getByText('"Minecraft server" already forwards this port.')).toBeTruthy();
    expect(save).toHaveProperty('disabled', true);
    const ports = within(sheet).getByRole('textbox', { name: 'Outside port' });
    await user.clear(ports);
    await user.type(ports, '9-3');
    expect(within(sheet).getByText('The range must end on a port after the first, up to 65535.')).toBeTruthy();
    await user.clear(ports);
    await user.type(ports, '8080');
    await user.type(within(sheet).getByRole('textbox', { name: /Inside port/ }), '99999');
    expect(within(sheet).getByText('Use a port from 1 to 65535, or leave it empty for the same port.')).toBeTruthy();
    expect(save).toHaveProperty('disabled', true);
  });

  it('shows the router’s answer next to its field', async () => {
    const fake = device(undefined, {
      savePortForwarding: async () => {
        throw new FieldErrors({ target: 'Give this device a fixed address first, so the rule always finds it.' });
      },
    });
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    const sheet = await addRule(user, 'desktop-office.lan');
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    expect(await within(sheet).findByText(/Give this device a fixed address first/)).toBeTruthy();
  });

  it('changes a rule in place, and turns it off', async () => {
    const fake = device();
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    await user.click(screen.getByRole('button', { name: 'Minecraft server' }));
    const sheet = screen.getByRole('dialog');
    expect(within(sheet).getByRole('heading', { name: 'Edit rule' })).toBeTruthy();
    expect((within(sheet).getByRole('textbox', { name: 'Outside port' }) as HTMLInputElement).value).toBe('25565');
    await user.click(within(sheet).getByRole('checkbox', { name: 'On' }));
    await user.click(within(sheet).getByRole('button', { name: 'Save' }));
    const saved = fake.savePortForwarding.mock.calls[0][0];
    expect(saved.map((rule) => rule.id)).toEqual(['pf-minecraft', 'pf-steam']);
    expect(saved[0]).toMatchObject({ name: 'Minecraft server', enabled: false, mac: DESKTOP });
  });

  it('keeps a device that has left in the menu, and shows the rule’s warning', async () => {
    const fake = device((router) => {
      router.portForwardingRules = [
        { id: 'gone', name: 'Console', protocol: 'udp', outsidePort: 22, mac: 'de:ad:be:ef:00:01', enabled: false },
      ];
    });
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    await user.click(screen.getByRole('button', { name: 'Console' }));
    const sheet = screen.getByRole('dialog');
    const menu = within(sheet).getByRole('combobox', { name: 'Goes to' }) as HTMLSelectElement;
    expect(menu.selectedOptions[0].textContent).toBe('de:ad:be:ef:00:01');
    expect(within(sheet).getByText(/Port 22 \(SSH\) is often attacked/)).toBeTruthy();
    // Off, so it needs no fixed address.
    expect(within(sheet).queryByText(/Saving also gives/)).toBeNull();
  });

  it('deletes a rule', async () => {
    const fake = device();
    const { user } = renderScreen(<PortForwardingScreen />, fake, PATH);
    await user.click(screen.getByRole('button', { name: 'Steam games' }));
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete rule' }));
    expect(fake.savePortForwarding.mock.calls[0][0].map((rule) => rule.id)).toEqual(['pf-minecraft']);
  });
});

describe('the demo’s port forwarding', () => {
  it('fills in each rule’s target as the router does', () => {
    const fake = fakeDevice((router) => {
      router.portForwardingRules.push({
        id: 'p',
        name: 'Files',
        protocol: 'tcp',
        outsidePort: 445,
        plugin: 'samba',
        enabled: true,
      });
    });
    const view = fake.portForwarding.data!;
    expect(view.rules[0]).toMatchObject({ target: 'desktop-office.lan', address: '192.168.8.131' });
    expect(view.rules[2]).toMatchObject({ target: 'Samba', address: '10.88.0.2' });
    expect(view.rules[2].warning).toMatch(/Port 445/);
  });
});
