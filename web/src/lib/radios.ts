// Which Wi-Fi radio does what: the same rules as the router (internal/radio AssignRoles), so the
// System page can show what "Automatic" will pick before saving, and the demo can act like it.
import type { Radio, RadioRole } from '@/data/types';

/** The owner's choice for one radio: a role, or "auto" to let RasBeacon pick. */
export type RadioChoice = RadioRole | 'auto';

export const ROLE_LABELS: Record<RadioRole, string> = {
  hotspot: 'Hotspot',
  uplink: 'Internet',
  guest: 'Guest network',
  unused: 'Not used',
};

/** "Built-in Wi-Fi" or "USB adapter 0e8d:7612". */
export function radioName(radio: Radio): string {
  return radio.isUsb ? `USB adapter${radio.usbId ? ` ${radio.usbId}` : ''}` : 'Built-in Wi-Fi';
}

/** The order automatic choices go through the radios: the hotspot's (ap0) first, then by phy. */
function autoOrder(radios: Radio[]): Radio[] {
  const phy = (radio: Radio) => {
    const number = Number(radio.phy.replace(/^phy/, ''));
    return radio.phy && Number.isInteger(number) ? number : Number.MAX_SAFE_INTEGER;
  };
  return [...radios].sort((a, b) => Number(b.interface === 'ap0') - Number(a.interface === 'ap0') || phy(a) - phy(b));
}

/**
 * Each radio's role for these choices (by MAC address): the chosen ones where they can work,
 * then the hotspot on the first USB radio that can run one (else any), and the internet on the
 * first radio left. The guest role is never picked automatically.
 */
export function assignRoles(radios: Radio[], choices: Record<string, RadioChoice>): Record<string, RadioRole> {
  const roles: Record<string, RadioRole> = {};
  const decided = new Set<string>();
  const taken = new Set<RadioRole>();
  for (const radio of radios) {
    const choice = choices[radio.mac] ?? 'auto';
    if (choice === 'auto') continue;
    if ((choice === 'hotspot' || choice === 'guest') && !radio.supportsAp) continue;
    if (choice !== 'unused' && taken.has(choice)) continue;
    roles[radio.mac] = choice;
    decided.add(radio.mac);
    if (choice !== 'unused') taken.add(choice);
  }
  const order = autoOrder(radios);
  if (!taken.has('hotspot')) {
    const free = order.filter((radio) => !decided.has(radio.mac) && radio.supportsAp);
    const hotspot = free.find((radio) => radio.isUsb) ?? free[0];
    if (hotspot) {
      roles[hotspot.mac] = 'hotspot';
      decided.add(hotspot.mac);
    }
  }
  if (!taken.has('uplink')) {
    const uplink = order.find((radio) => !decided.has(radio.mac));
    if (uplink) {
      roles[uplink.mac] = 'uplink';
      decided.add(uplink.mac);
    }
  }
  for (const radio of radios) roles[radio.mac] ??= 'unused';
  return roles;
}

/** Why these choices can't be saved, as the router says it, or undefined. */
export function rolesProblem(radios: Radio[], choices: Record<string, RadioChoice>): string | undefined {
  const chosen = Object.values(choices).filter((choice) => choice !== 'auto' && choice !== 'unused');
  if (new Set(chosen).size !== chosen.length) return 'Give each job to one radio only.';
  if (!Object.values(assignRoles(radios, choices)).includes('hotspot')) {
    return 'One radio must run the hotspot, or you couldn’t reach RasBeacon.';
  }
  return undefined;
}

/** What to know about these roles before saving them. */
export function rolesNotes(radios: Radio[], roles: Record<string, RadioRole>): string[] {
  const notes: string[] = [];
  const hotspot = radios.find((radio) => roles[radio.mac] === 'hotspot');
  if (hotspot && !hotspot.isUsb) notes.push('The built-in Wi-Fi holds only about 8 devices as a hotspot.');
  if (!Object.values(roles).includes('uplink')) {
    notes.push('No radio joins Wi-Fi networks, so the internet comes only from Ethernet, a phone or a modem.');
  }
  return notes;
}

/** The choices saved now: the chosen role, or "auto". */
export function savedChoices(radios: Radio[]): Record<string, RadioChoice> {
  return Object.fromEntries(radios.map((radio) => [radio.mac, radio.chosen ? radio.role : 'auto']));
}

/** "2 min 41 s" (or "41 s") for the seconds left to keep new roles. */
export function timeLeft(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  // Non-breaking spaces keep each number with its unit.
  return minutes > 0 ? `${minutes}\u00a0min ${rest}\u00a0s` : `${rest}\u00a0s`;
}
