import { useDevice } from '@/data/device';
import type { VPNStatus } from '@/data/types';
import { formatAgo, formatBytes } from '@/lib/format';
import { Badge, BreakableText, LinkRow, Row, Section, SwitchRow } from '@/ui';
import { vpnStateView } from '../common/describe';

/** The VPN's two switches, the profile in use and the tunnel's readings. */
export function VpnSection({ vpn }: { vpn: VPNStatus }) {
  const { vpnSettings, setVpnEnabled, setKillSwitch } = useDevice();
  const view = vpnStateView(vpn);
  const profile = vpnSettings.data?.activeProfile;

  return (
    <Section
      title="VPN"
      action={
        <Badge tone={view.tone} pulse={view.pulse}>
          {view.badge}
        </Badge>
      }
    >
      <SwitchRow
        leading={{ tile: 'vpn_lock', color: 'teal' }}
        title="Use the VPN"
        subtitle="Send your devices' traffic through WireGuard"
        checked={vpn.enabled}
        onChange={setVpnEnabled}
        disabled={!profile}
      />
      {profile && <LinkRow to="/vpn" title="Profile" value={<BreakableText>{profile}</BreakableText>} />}
      {/* With the tunnel off, the last handshake no longer says anything about it. */}
      <Row
        title="Last handshake"
        value={!vpn.enabled ? 'Inactive' : vpn.handshakeAgeSeconds < 0 ? 'Never' : formatAgo(vpn.handshakeAgeSeconds)}
      />
      {vpn.enabled && (
        <Row title="Server" value={vpn.endpoint ? <BreakableText>{vpn.endpoint}</BreakableText> : 'not reached yet'} />
      )}
      <Row
        title="Transferred"
        value={
          <span className="tabular">
            ↓&nbsp;{formatBytes(vpn.receivedBytes)} ↑&nbsp;{formatBytes(vpn.sentBytes)}
          </span>
        }
      />
      <SwitchRow
        leading={{ tile: 'block', color: 'red' }}
        title="Kill switch"
        subtitle="Block traffic if the VPN drops"
        checked={vpn.killSwitch}
        onChange={setKillSwitch}
      />
    </Section>
  );
}
