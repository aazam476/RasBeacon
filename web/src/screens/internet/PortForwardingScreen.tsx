import { useState } from 'react';
import { useDevice } from '@/data/device';
import type { PortForwardingSettings, PortForwardRule } from '@/data/types';
import { MAX_PORT_FORWARDS, ruleSummary } from '@/lib/portForward';
import { Loaded } from '@/shell/Loaded';
import { usePageTitle } from '@/shell/navigation';
import { BreakableText, ButtonRow, Notice, NoticeList, Row, Section, SheetRow } from '@/ui';
import { PortForwardSheet } from './PortForwardSheet';

/**
 * Internet → Port forwarding: connections from the internet to a port on RasBeacon's outside
 * address go to a device on the hotspot or to a plugin (a game server, a camera, a file share).
 */
export function PortForwardingScreen() {
  usePageTitle('Port forwarding');
  const { portForwarding, reload } = useDevice();
  return (
    <Loaded data={portForwarding} what="the port forwarding rules" onRetry={() => reload('portForwarding')}>
      {(settings) => <Rules settings={settings} />}
    </Loaded>
  );
}

/** What the reachability means for the rules, or null when the internet can reach them. */
function ReachabilityNotice({ settings }: { settings: PortForwardingSettings }) {
  switch (settings.reachability) {
    case 'private':
      return (
        <Notice tone="warning" title="RasBeacon is behind another router">
          <p>
            Its outside address is a private one, so the internet only reaches your rules if that router forwards the
            same ports to{' '}
            <span className="font-mono">
              <BreakableText>{settings.outsideAddress ?? ''}</BreakableText>
            </span>{' '}
            first. Hotel and public networks usually don’t let you.
          </p>
        </Notice>
      );
    case 'shared':
      return (
        <Notice tone="warning" title="The internet can’t reach this network">
          <p>
            This network shares one address between many customers, so connections from the internet can’t find
            RasBeacon. Your rules work again on another network, or through a VPN that forwards ports.
          </p>
        </Notice>
      );
    case 'offline':
      return (
        <Notice title="Not connected to the internet">
          <p>Your rules start working once RasBeacon is connected.</p>
        </Notice>
      );
    default:
      return null;
  }
}

function Rules({ settings }: { settings: PortForwardingSettings }) {
  // The rule being edited, or "new" while adding one.
  const [editing, setEditing] = useState<PortForwardRule | 'new' | null>(null);
  const warnings = settings.rules.filter((rule) => rule.enabled && rule.warning);
  const full = settings.rules.length >= MAX_PORT_FORWARDS;

  return (
    <>
      <ReachabilityNotice settings={settings} />
      {settings.outsideAddress && (
        <Section footer="Devices on the internet reach your rules at this address. Devices on your hotspot can use it too.">
          <Row
            title="Outside address"
            value={
              <span className="font-mono">
                <BreakableText>{settings.outsideAddress}</BreakableText>
              </span>
            }
          />
        </Section>
      )}
      <Section
        title="Rules"
        footer="Anyone on the internet can reach what listens on a forwarded port, so forward only what you need and keep it protected by a password. The VPN doesn’t carry forwarded connections."
      >
        {settings.rules.map((rule) => (
          <SheetRow
            key={rule.id}
            title={rule.name}
            value={rule.enabled ? undefined : 'Off'}
            subtitle={`${ruleSummary(rule)} to ${rule.target ?? rule.mac ?? rule.plugin}`}
            onClick={() => setEditing(rule)}
          />
        ))}
        <ButtonRow
          leading={{ icon: 'add', className: 'text-tint' }}
          onClick={() => setEditing('new')}
          disabled={full}
          note={full && `RasBeacon keeps at most ${MAX_PORT_FORWARDS} rules.`}
        >
          Add rule
        </ButtonRow>
      </Section>
      {warnings.length > 0 && (
        <NoticeList
          tone="warning"
          ariaLabel="Warnings"
          messages={warnings.map((rule) => `${rule.name}: ${rule.warning}`)}
        />
      )}
      {editing && (
        <PortForwardSheet
          rule={editing === 'new' ? null : editing}
          rules={settings.rules}
          ipv6Prefix={settings.ipv6Prefix}
          onClose={() => setEditing(null)}
        />
      )}
    </>
  );
}
