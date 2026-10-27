// Which Wi-Fi radio does what: the same rules as the router's radio.AssignRoles.
import { describe, expect, it } from 'vitest';
import type { Radio } from '@/data/types';
import { assignRoles, radioName, rolesNotes, rolesProblem, savedChoices, timeLeft } from '@/lib/radios';

const radio = (mac: string, phy: string, extra: Partial<Radio> = {}): Radio => ({
  interface: `wlan${phy.slice(3)}`,
  mac,
  phy,
  driver: 'mt76x2u',
  isUsb: true,
  usbId: '0e8d:7612',
  supportsAp: true,
  maxAps: 2,
  role: 'unused',
  ...extra,
});
const builtIn = radio('b8:27:eb:00:00:01', 'phy0', { isUsb: false, usbId: '', driver: 'brcmfmac' });
const usbA = radio('00:0e:8e:00:00:02', 'phy1');
const usbB = radio('00:0e:8e:00:00:03', 'phy2');
const noAp = radio('00:c0:ca:00:00:04', 'phy3', { supportsAp: false });
const radios = [builtIn, usbA, usbB, noAp];

describe('radio roles', () => {
  it('picks like the router when nothing is chosen', () => {
    expect(assignRoles(radios, {})).toEqual({
      [builtIn.mac]: 'uplink',
      [usbA.mac]: 'hotspot',
      [usbB.mac]: 'unused',
      [noAp.mac]: 'unused',
    });
  });

  it('keeps the hotspot that runs now (ap0) when picking by itself', () => {
    const now = [builtIn, usbA, { ...usbB, interface: 'ap0' }];
    expect(assignRoles(now, {})[usbB.mac]).toBe('hotspot');
  });

  it('follows the owner’s choices, and picks the rest', () => {
    expect(assignRoles(radios, { [usbB.mac]: 'hotspot', [usbA.mac]: 'guest', [builtIn.mac]: 'auto' })).toEqual({
      [builtIn.mac]: 'uplink',
      [usbA.mac]: 'guest',
      [usbB.mac]: 'hotspot',
      [noAp.mac]: 'unused',
    });
  });

  it('skips a choice that can’t work', () => {
    expect(assignRoles(radios, { [noAp.mac]: 'hotspot' })[noAp.mac]).toBe('unused');
    expect(assignRoles(radios, { [usbA.mac]: 'uplink', [usbB.mac]: 'uplink' })[usbB.mac]).toBe('hotspot');
  });

  it('falls back to the built-in Wi-Fi without an adapter, and leaves out radios with no phy last', () => {
    expect(assignRoles([builtIn, { ...usbA, phy: '' }], { [usbA.mac]: 'unused' })[builtIn.mac]).toBe('hotspot');
  });

  it('refuses two radios for one job, and no hotspot at all', () => {
    expect(rolesProblem(radios, { [usbA.mac]: 'hotspot', [usbB.mac]: 'hotspot' })).toBe(
      'Give each job to one radio only.',
    );
    expect(rolesProblem(radios, { [builtIn.mac]: 'unused', [usbA.mac]: 'unused', [usbB.mac]: 'unused' })).toMatch(
      /One radio must run the hotspot/,
    );
    expect(rolesProblem(radios, { [usbB.mac]: 'hotspot' })).toBeUndefined();
  });

  it('says what to know before saving', () => {
    expect(
      rolesNotes(
        radios,
        assignRoles(radios, {
          [builtIn.mac]: 'hotspot',
          [usbA.mac]: 'unused',
          [usbB.mac]: 'unused',
          [noAp.mac]: 'unused',
        }),
      ),
    ).toEqual([
      'The built-in Wi-Fi holds only about 8 devices as a hotspot.',
      'No radio joins Wi-Fi networks, so the internet comes only from Ethernet, a phone or a modem.',
    ]);
    expect(rolesNotes(radios, assignRoles(radios, {}))).toEqual([]);
  });

  it('names radios and reads the saved choices', () => {
    expect(radioName(builtIn)).toBe('Built-in Wi-Fi');
    expect(radioName(usbA)).toBe('USB adapter 0e8d:7612');
    expect(radioName({ ...usbA, usbId: '' })).toBe('USB adapter');
    expect(savedChoices([{ ...usbA, role: 'guest', chosen: true }, builtIn])).toEqual({
      [usbA.mac]: 'guest',
      [builtIn.mac]: 'auto',
    });
  });

  it('shows the time left', () => {
    expect(timeLeft(161)).toBe('2 min 41 s');
    expect(timeLeft(41)).toBe('41 s');
  });
});
