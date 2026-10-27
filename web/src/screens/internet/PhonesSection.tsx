import { useDevice } from '@/data/device';
import type { PhoneUplinkStatus } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { Row, Section, SwitchRow } from '@/ui';
import { phoneName } from '../common/describe';

/**
 * Phones sharing their connection over USB and USB modems: RasBeacon uses them for the internet
 * when neither Ethernet nor Wi-Fi has it. Shown only when one is plugged in.
 */
export function PhonesSection({ phones, metered }: { phones: PhoneUplinkStatus[]; metered: boolean }) {
  const { setPhoneMetered } = useDevice();
  if (phones.length === 0) return null;

  return (
    <Section
      title="Phones and modems"
      footer="Used for the internet when neither Ethernet nor Wi-Fi has it. On an iPhone, turn on Personal Hotspot and tap Trust when it asks; on Android, turn on USB tethering. Hotel login pages, the VPN and the kill switch work as on any connection; port forwarding usually doesn't."
    >
      {phones.map((phone) => (
        <Row
          key={phone.interface}
          leading={{ icon: 'usb' }}
          title={phoneName(phone)}
          subtitle={`↓ ${formatBytes(phone.receivedBytes)} ↑ ${formatBytes(phone.sentBytes)} since plugged in`}
          value={phone.connected ? 'Connected' : 'Waiting for an address'}
        />
      ))}
      <SwitchRow
        title="Treat as metered"
        subtitle="Asks your devices to hold back big downloads and backups while a phone or modem carries the internet."
        checked={metered}
        onChange={setPhoneMetered}
      />
    </Section>
  );
}
