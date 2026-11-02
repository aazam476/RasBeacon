import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status, UplinkSettings } from '@/data/types';
import { PageFormProvider } from '@/lib/PageFormProvider';
import { Loaded } from '@/shell/Loaded';
import { PortalNotice } from '../common/PortalNotice';
import { AddNetworkSection } from './AddNetworkSection';
import { AddNetworkSheet, type AddNetworkStart } from './AddNetworkSheet';
import { ConnectionSection } from './ConnectionSection';
import { DnsSection } from './DnsSection';
import { EthernetSection } from './EthernetSection';
import { InternetSpeedSection } from './InternetSpeedSection';
import { LANPortsSection } from './LANPortsSection';
import { MacAddressSection } from './MacAddressSection';
import { PhonesSection } from './PhonesSection';
import { SavedNetworksSection } from './SavedNetworksSection';
import { SpeedtestSection } from './SpeedtestSection';
import { UnsavedChangesBar } from './UnsavedChangesBar';
import { useNetworkDraft } from './useNetworkDraft';

/**
 * The connection in use and how RasBeacon gets the internet, in the order most people need
 * them: the connection and its speed, the saved Wi-Fi networks, the Ethernet port, phones and
 * modems, adding a network, then the MAC address and DNS servers few people change. Every change
 * is saved with the one bar at the bottom.
 */
export function InternetScreen() {
  const { status, uplinkSettings, refreshStatus, reload } = useDevice();

  return (
    <PageFormProvider>
      <Loaded data={status} what="the connection" onRetry={refreshStatus}>
        {(status) => (
          <>
            {status.portal.detected && <PortalNotice portal={status.portal} />}
            <ConnectionSection status={status} />
            <SpeedtestSection status={status} />
            <InternetSpeedSection lastTest={status.speedtest.last} />
          </>
        )}
      </Loaded>
      <Loaded data={uplinkSettings} what="the saved networks" onRetry={() => reload('uplinkSettings')}>
        {(settings) => <NetworkSettings settings={settings} status={status.data} />}
      </Loaded>
      <DnsSection />
      {/* Last on the page, so it can stay pinned to the bottom of the screen. */}
      <UnsavedChangesBar />
    </PageFormProvider>
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
      {settings.ethernetPresent && (
        <EthernetSection
          role={settings.ethernetRole}
          port={status?.uplink.ethernet}
          diagnostics={status?.ethernetDiagnostics}
        />
      )}
      <LANPortsSection ports={status?.lanPorts ?? []} />
      <PhonesSection
        phones={status?.uplink.phones ?? []}
        modem={status?.uplink.modem}
        modemSettings={modemSettings.data}
        metered={settings.phoneMetered ?? true}
        mobileData={status?.uplink.mobileData}
      />
      <AddNetworkSection
        draft={draft}
        wifiUplinkAvailable={settings.wifiUplinkAvailable}
        connectedSsid={connectedSsid}
        onAdd={setAdding}
      />
      <MacAddressSection settings={settings} clients={status?.clients ?? []} />
      {adding && (
        <AddNetworkSheet start={adding} list={draft.networks} onAdd={draft.add} onClose={() => setAdding(null)} />
      )}
    </>
  );
}
