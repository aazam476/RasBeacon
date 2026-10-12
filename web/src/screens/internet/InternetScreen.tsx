import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { Status, UplinkSettings } from '@/data/types';
import { Loaded } from '@/shell/Loaded';
import { PortalNotice } from '../common/PortalNotice';
import { AddNetworkSection } from './AddNetworkSection';
import { AddNetworkSheet, type AddNetworkStart } from './AddNetworkSheet';
import { ConnectionSection } from './ConnectionSection';
import { EthernetSection } from './EthernetSection';
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
      {adding && (
        <AddNetworkSheet start={adding} list={draft.networks} onAdd={draft.add} onClose={() => setAdding(null)} />
      )}
      {/* Last on the page, so it can stay pinned to the bottom of the screen. */}
      <UnsavedChangesBar draft={draft} />
    </>
  );
}
