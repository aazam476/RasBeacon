import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status, UplinkSettings } from '@/data/types';
import { Loaded } from '@/shell/Loaded';
import { LinkRow, Section } from '@/ui';
import { PortalNotice } from '../common/PortalNotice';
import { AddNetworkSection } from './AddNetworkSection';
import { AddNetworkSheet, type AddNetworkStart } from './AddNetworkSheet';
import { ConnectionSection } from './ConnectionSection';
import { DnsSection } from './DnsSection';
import { EthernetSection } from './EthernetSection';
import { InternetSpeedSection } from './InternetSpeedSection';
import { LANPortsSection } from './LANPortsSection';
import { MacAddressSection } from './MacAddressSection';
import { ModemSection } from './ModemSection';
import { MobileDataSection } from './MobileDataSection';
import { PhonesSection } from './PhonesSection';
import { SavedNetworksSection } from './SavedNetworksSection';
import { SpeedtestSection } from './SpeedtestSection';
import { UnsavedChangesBar } from './UnsavedChangesBar';
import { useNetworkDraft } from './useNetworkDraft';

/** The connection in use, the Wi-Fi networks RasBeacon joins, and what the Ethernet port is for. */
export function InternetScreen() {
  const { status, uplinkSettings, refreshStatus, reload } = useDevice();

  return (
    <>
      <Loaded data={status} what="the connection" onRetry={refreshStatus}>
        {(status) => (
          <>
            {status.portal.detected && <PortalNotice portal={status.portal} />}
            <ConnectionSection status={status} />
            <SpeedtestSection status={status} />
          </>
        )}
      </Loaded>
      <Loaded data={uplinkSettings} what="the saved networks" onRetry={() => reload('uplinkSettings')}>
        {(settings) => <NetworkSettings settings={settings} status={status.data} />}
      </Loaded>
    </>
  );
}

function NetworkSettings({ settings, status }: { settings: UplinkSettings; status: Status | undefined }) {
  const { modemSettings } = useDevice();
  const draft = useNetworkDraft(settings);
  const [adding, setAdding] = useState<AddNetworkStart | null>(null);
  const connectedSsid = status?.uplink.connected && status.uplink.kind === 'wifi' ? status.uplink.wifi.ssid : '';

  return (
    <>
      <SavedNetworksSection
        draft={draft}
        wifiUplinkAvailable={settings.wifiUplinkAvailable}
        connectedSsid={connectedSsid}
      />
      <AddNetworkSection
        draft={draft}
        wifiUplinkAvailable={settings.wifiUplinkAvailable}
        connectedSsid={connectedSsid}
        onAdd={setAdding}
      />
      {settings.ethernetPresent && (
        <EthernetSection
          role={settings.ethernetRole}
          port={status?.uplink.ethernet}
          diagnostics={status?.ethernetDiagnostics}
        />
      )}
      <MacAddressSection settings={settings} clients={status?.clients ?? []} />
      {modemSettings.data && <ModemSection modem={status?.uplink.modem} settings={modemSettings.data} />}
      <PhonesSection phones={status?.uplink.phones ?? []} metered={settings.phoneMetered ?? true} />
      <MobileDataSection data={status?.uplink.mobileData} />
      <LANPortsSection ports={status?.lanPorts ?? []} />
      <DnsSection />
      <InternetSpeedSection lastTest={status?.speedtest.last} />
      <PortForwardingLink />
      {adding && (
        <AddNetworkSheet start={adding} list={draft.networks} onAdd={draft.add} onClose={() => setAdding(null)} />
      )}
      {/* Last on the page, so it can stay pinned to the bottom of the screen. */}
      <UnsavedChangesBar draft={draft} />
    </>
  );
}

/** The way to Port forwarding, with how many rules are on. */
function PortForwardingLink() {
  const { portForwarding } = useDevice();
  const on = portForwarding.data?.rules.filter((rule) => rule.enabled).length;
  let value: string | undefined;
  if (on !== undefined) value = on === 0 ? 'Off' : on === 1 ? '1 rule' : `${on} rules`;
  return (
    <Section footer="Lets devices on the internet reach a game server, a camera or a plugin on your hotspot.">
      <LinkRow to="/internet/port-forwarding" leading={{ icon: 'swap_horiz' }} title="Port forwarding" value={value} />
    </Section>
  );
}
