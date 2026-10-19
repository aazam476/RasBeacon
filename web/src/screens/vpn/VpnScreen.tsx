import { useDevice } from '@/data/device';
import type { VPNSettings, VPNStatus } from '@/data/types';
import { formatAgo, formatBytes } from '@/lib/format';
import { Loaded } from '@/shell/Loaded';
import { BreakableText, Notice, Row, Section, SwitchRow, type NoticeTone, type Tone } from '@/ui';
import { vpnStateView } from '../common/describe';
import { ImportProfileSection } from './ImportProfileSection';
import { ProfilesSection } from './ProfilesSection';
import { PublicKeySection, ServerDetailsSection } from './ServerDetails';

/** The tunnel and the kill switch, the WireGuard profiles, importing one, and its server details. */
export function VpnScreen() {
  const { status, vpnSettings, refreshStatus, reload } = useDevice();

  return (
    <Loaded data={vpnSettings} what="the VPN settings" onRetry={() => reload('vpnSettings')}>
      {(settings) => {
        const active = settings.profiles.find((profile) => profile.name === settings.activeProfile);
        return (
          <>
            <Loaded data={status} what="the VPN state" onRetry={refreshStatus}>
              {(status) => <TunnelSections vpn={status.vpn} settings={settings} />}
            </Loaded>
            <ProfilesSection settings={settings} />
            <ImportProfileSection />
            {active && <ServerDetailsSection profile={active} />}
            {active && <PublicKeySection profile={active} />}
          </>
        );
      }}
    </Loaded>
  );
}

const NOTICE_TONES: Record<Tone, NoticeTone> = {
  green: 'success',
  orange: 'warning',
  red: 'danger',
  gray: 'neutral',
  tint: 'info',
};

function TunnelSections({ vpn, settings }: { vpn: VPNStatus; settings: VPNSettings }) {
  const { setVpnEnabled, setKillSwitch } = useDevice();
  const view = vpnStateView(vpn);
  const configured = settings.profiles.length > 0;

  return (
    <>
      {configured ? (
        <Notice
          tone={NOTICE_TONES[view.tone]}
          icon={view.icon}
          title={view.title}
          role={view.alert ? 'alert' : undefined}
        >
          <p>{view.summary}</p>
        </Notice>
      ) : (
        <Notice tone="info" title="No VPN set up yet">
          <p>Import a WireGuard file first: your VPN provider or WireGuard server gives you one.</p>
        </Notice>
      )}

      <Section>
        <SwitchRow
          leading={{ tile: 'vpn_lock', color: 'teal' }}
          title="Use the VPN"
          subtitle="Send all your devices' traffic through WireGuard"
          checked={vpn.enabled}
          onChange={setVpnEnabled}
          disabled={!configured}
        />
        {vpn.enabled && (
          <>
            <Row
              title="Last handshake"
              value={vpn.handshakeAgeSeconds < 0 ? 'No handshake yet' : formatAgo(vpn.handshakeAgeSeconds)}
            />
            <Row
              title="Server"
              value={vpn.endpoint ? <BreakableText>{vpn.endpoint}</BreakableText> : 'not reached yet'}
            />
          </>
        )}
        <Row title="Received" value={formatBytes(vpn.receivedBytes)} />
        <Row title="Sent" value={formatBytes(vpn.sentBytes)} />
      </Section>

      <Section>
        {/* Without a profile there is no VPN to keep traffic in, so the switch would do nothing. */}
        <SwitchRow
          leading={{ tile: 'block', color: 'red' }}
          title="Kill switch"
          subtitle={
            vpn.killSwitch
              ? "Devices only use the internet through the VPN, and have none while it's down or off."
              : 'If the VPN goes down, devices use the internet directly.'
          }
          checked={configured && vpn.killSwitch}
          onChange={setKillSwitch}
          disabled={!configured}
        />
      </Section>
    </>
  );
}
