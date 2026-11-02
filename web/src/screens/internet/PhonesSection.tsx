import { useDevice } from '@/data/device';
import type { MobileData, ModemSettings, ModemStatus, PhoneUplinkStatus } from '@/data/types';
import { formatBytes } from '@/lib/format';
import { Row, Section, SwitchRow } from '@/ui';
import { phoneName } from '../common/describe';
import { MobileDataRows } from './MobileDataRows';
import { NO_MOBILE_DATA, sinceLabel } from './mobileData';
import { ModemProblem, ModemSettingsRows, ModemStatusRows } from './ModemRows';

interface PhonesSectionProps {
  /** Phones sharing their connection and HiLink modems on USB. */
  phones: PhoneUplinkStatus[];
  /** The modem that needs a modem manager, if one is plugged in. */
  modem: ModemStatus | undefined;
  /** Its settings, once loaded. */
  modemSettings: ModemSettings | undefined;
  /** Whether a phone or modem connection counts as metered. */
  metered: boolean;
  /** This month's mobile data (absent from an older RasBeacon). */
  mobileData: MobileData | undefined;
}

/**
 * Internet from a phone or a modem, and the data they use: the phones sharing their connection
 * over USB and the modems plugged in (or that none is), whether such a connection counts as
 * metered, and this month's data with its limit. Always shown, so it is there to set up before a
 * trip and easy to find when a phone is plugged in.
 */
export function PhonesSection({ phones, modem, modemSettings, metered, mobileData }: PhonesSectionProps) {
  const { setPhoneMetered } = useDevice();
  const data = mobileData ?? NO_MOBILE_DATA;

  return (
    <>
      {modem && <ModemProblem modem={modem} />}
      <Section
        title="Phones and modems"
        footer={`Used for the internet when neither Ethernet nor Wi-Fi has it. On an iPhone, turn on Personal Hotspot and tap Trust when it asks; on Android, turn on USB tethering. Data is counted since ${sinceLabel(data.since)}; the Status page warns you as you near the limit, and nothing is blocked.`}
      >
        {phones.map((phone) => (
          <Row
            key={phone.interface}
            leading={{ icon: 'usb' }}
            title={phoneName(phone)}
            subtitle={`↓ ${formatBytes(phone.receivedBytes)} ↑ ${formatBytes(phone.sentBytes)} since plugged in`}
            value={phone.connected ? 'Connected' : 'Waiting for an address'}
          />
        ))}
        {modem && <ModemStatusRows modem={modem} />}
        {phones.length === 0 && !modem && (
          <Row leading={{ icon: 'usb' }} title={<span className="text-label-2">No phone or modem plugged in</span>} />
        )}
        <SwitchRow
          title="Treat as metered"
          subtitle="Asks your devices to hold back big downloads and backups while a phone or modem carries the internet."
          checked={metered}
          onChange={setPhoneMetered}
        />
        <MobileDataRows data={data} />
        {modem && modemSettings && <ModemSettingsRows modem={modem} settings={modemSettings} />}
      </Section>
    </>
  );
}
