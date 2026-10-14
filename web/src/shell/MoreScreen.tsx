import { useDevice, type DeviceState } from '@/data/device';
import { LinkRow, Section } from '@/ui';
import { MORE_ITEMS, type NavItem } from './navigation';

/** How each page stands, in a word or two, from the data at hand; nothing until it is known. */
function summary(item: NavItem, device: DeviceState): string | undefined {
  const status = device.status.data;
  switch (item.path) {
    case '/hotspot':
      return status?.hotspot.ssid;
    case '/guests':
      if (!status) return undefined;
      if (!status.guest.enabled) return 'Off';
      return status.guest.clients.length === 1 ? '1 guest' : `${status.guest.clients.length} guests`;
    case '/ssh':
      return device.sshSettings.data && (device.sshSettings.data.enabled ? 'On' : 'Off');
    case '/https':
      return device.httpsSettings.data && (device.httpsSettings.data.enabled ? 'On' : 'Off');
    case '/plugins': {
      const count = status?.plugins.length;
      if (count === undefined) return undefined;
      return count === 0 ? 'None' : count === 1 ? '1 installed' : `${count} installed`;
    }
    case '/system':
      if (!status) return undefined;
      return status.update.latestVersion ? `${status.update.latestVersion} available` : status.version;
    default:
      return undefined;
  }
}

/**
 * The phone's More tab: the pages without a tab of their own, each on its tile with how it
 * stands, as in the Settings app.
 */
export function MoreScreen() {
  const device = useDevice();

  return (
    <Section>
      {MORE_ITEMS.map((item) => (
        <LinkRow
          key={item.path}
          to={item.path}
          leading={{ tile: item.icon, color: item.tile }}
          title={item.label}
          value={summary(item, device)}
        />
      ))}
    </Section>
  );
}
