import { useId, useState } from 'react';
import type { SavedNetwork } from '@/data/types';
import {
  BreakableText,
  Button,
  FormError,
  IconButton,
  Notice,
  PasswordField,
  Row,
  RowText,
  Section,
  FormSwitch,
  Sheet,
} from '@/ui';
import { methodName, SECURITY_NAMES } from './networkDisplay';
import type { NetworkDraft } from './useNetworkDraft';

interface SavedNetworksSectionProps {
  draft: NetworkDraft;
  /** False when no radio is free to join Wi-Fi networks. */
  wifiUplinkAvailable: boolean;
  /** The network RasBeacon is connected to, if any. */
  connectedSsid: string;
}

/**
 * The Wi-Fi networks RasBeacon joins, most preferred first: it uses a plugged-in Ethernet cable
 * when there is one, otherwise the first network in this list that is in range. Changes are
 * saved together, with the bar at the bottom of the page.
 */
export function SavedNetworksSection({ draft, wifiUplinkAvailable, connectedSsid }: SavedNetworksSectionProps) {
  const [changingPassword, setChangingPassword] = useState<number | null>(null);
  const { networks } = draft;

  return (
    <>
      {!wifiUplinkAvailable && (
        <Notice tone="warning" icon="wifi_off" role="alert">
          <p>
            No Wi-Fi radio is free to join a network: the only radio runs the hotspot. Plug in a USB Wi-Fi adapter for
            the hotspot, or use Ethernet.
          </p>
        </Notice>
      )}
      <Section
        title="Wi-Fi networks to join"
        footer="Most preferred first. RasBeacon uses a plugged-in Ethernet cable first, otherwise the first network in range. With a random MAC address, the network sees a made-up address instead of the Pi's, new at each restart."
      >
        {networks.length === 0 ? (
          <Row title={<span className="text-label-2">No networks saved yet. Add one below.</span>} />
        ) : (
          <ol className="rows">
            {networks.map((network, index) => (
              <NetworkRow
                key={network.id || `new-${network.ssid}`}
                network={network}
                position={index + 1}
                isFirst={index === 0}
                isLast={index === networks.length - 1}
                connected={network.ssid === connectedSsid}
                error={draft.errorAt(index)}
                onMove={(delta) => draft.move(index, delta)}
                onRemove={() => draft.remove(index)}
                onChangePassword={() => setChangingPassword(index)}
                onRandomMac={(randomMac) => draft.setRandomMac(index, randomMac)}
              />
            ))}
          </ol>
        )}
      </Section>

      {changingPassword !== null && networks[changingPassword] && (
        <ChangePasswordSheet
          network={networks[changingPassword]}
          onChange={(password) => draft.setPassword(changingPassword, password)}
          onClose={() => setChangingPassword(null)}
        />
      )}
    </>
  );
}

interface NetworkRowProps {
  network: SavedNetwork;
  /** 1 = most preferred. */
  position: number;
  isFirst: boolean;
  isLast: boolean;
  connected: boolean;
  error?: string;
  onMove: (delta: -1 | 1) => void;
  onRemove: () => void;
  onChangePassword: () => void;
  onRandomMac: (randomMac: boolean) => void;
}

function NetworkRow({
  network,
  position,
  isFirst,
  isLast,
  connected,
  error,
  onMove,
  onRemove,
  onChangePassword,
  onRandomMac,
}: NetworkRowProps) {
  const randomMacId = useId();
  const isNew = network.id === '';
  const details = [SECURITY_NAMES[network.security], network.hidden && 'Hidden', network.password && 'New password']
    .filter(Boolean)
    .join(' · ');

  return (
    <li className="row flex-col items-stretch gap-2.5">
      <div className="flex items-center gap-3 lg:gap-2.5">
        {/* The place in the order, where other rows have their tile. */}
        <span
          aria-hidden="true"
          className="row-tile tabular grid size-[1.8125rem] shrink-0 place-items-center rounded-full bg-fill-2 text-footnote font-semibold text-label-2 lg:size-6"
        >
          {position}
        </span>
        <RowText
          title={<BreakableText>{network.ssid}</BreakableText>}
          subtitle={
            <>
              {connected && <span className="font-semibold text-green">Connected · </span>}
              {details}
              {isNew && <span className="text-orange"> (not saved yet)</span>}
            </>
          }
        />
        {/* A list of one has no order to change. */}
        {!(isFirst && isLast) && (
          <span className="-mr-1 flex shrink-0 items-center gap-1.5">
            <IconButton
              icon="arrow_upward"
              ariaLabel={`Move ${network.ssid} up`}
              disabled={isFirst}
              onClick={() => onMove(-1)}
            />
            <IconButton
              icon="arrow_downward"
              ariaLabel={`Move ${network.ssid} down`}
              disabled={isLast}
              onClick={() => onMove(1)}
            />
          </span>
        )}
      </div>

      <div className="flex flex-col gap-2.5 pl-[2.5625rem] lg:pl-[2.125rem]">
        {network.enterprise && <EnterpriseSummary network={network} />}
        <label className="flex cursor-pointer items-center gap-3">
          <span id={randomMacId} className="min-w-0 flex-1 text-subhead text-label">
            Random MAC address
          </span>
          <FormSwitch checked={network.randomMac ?? false} onChange={onRandomMac} labelledBy={randomMacId} />
        </label>
        <div className="-my-1 flex flex-wrap gap-x-5">
          {network.security !== 'open' && !isNew && (
            <Button
              variant="plain"
              size="sm"
              onClick={onChangePassword}
              aria-label={`Change password for ${network.ssid}`}
            >
              Change password
            </Button>
          )}
          <Button variant="plain-danger" size="sm" onClick={onRemove} aria-label={`Remove ${network.ssid}`}>
            Remove
          </Button>
        </div>
        {error && <FormError>{error}</FormError>}
      </div>
    </li>
  );
}

/** The Enterprise login of a saved network, with a warning when the login server isn't checked. */
function EnterpriseSummary({ network }: { network: SavedNetwork }) {
  const enterprise = network.enterprise!;
  const items: [string, string][] = [
    ['Username', enterprise.identity],
    ['Method', methodName(enterprise.method)],
  ];
  if (!enterprise.allowNoCertificateCheck) {
    items.push(['Server', enterprise.serverName], ['CA certificate', enterprise.caCertSummary || 'none']);
  }
  if (enterprise.clientCertSummary) items.push(['Your certificate', enterprise.clientCertSummary]);

  // Labels beside their values where there is room; on phones above them, so long values get the full width.
  return (
    <div className="@container flex flex-col gap-1.5">
      <dl className="flex flex-col gap-1.5 text-footnote @[22rem]:grid @[22rem]:grid-cols-[auto_minmax(0,1fr)] @[22rem]:gap-x-3 @[22rem]:gap-y-0.5">
        {items.map(([label, value]) => (
          <div key={label} className="@[22rem]:contents">
            <dt className="text-label-2">{label}</dt>
            <dd className="selectable wrap-anywhere text-label">
              <BreakableText>{value}</BreakableText>
            </dd>
          </div>
        ))}
      </dl>
      {enterprise.allowNoCertificateCheck && (
        <p className="text-footnote font-semibold text-red">Login server is NOT checked.</p>
      )}
    </div>
  );
}

interface ChangePasswordSheetProps {
  network: SavedNetwork;
  onChange: (password: string) => void;
  onClose: () => void;
}

/** A new password for a saved network. It is saved with the list. */
function ChangePasswordSheet({ network, onChange, onClose }: ChangePasswordSheetProps) {
  const [password, setPassword] = useState(network.password ?? '');
  const passphrase = network.enterprise?.method === 'tls';

  return (
    <Sheet
      title={passphrase ? 'Key passphrase' : 'Password'}
      confirmLabel="Done"
      busyLabel="Saving…"
      onConfirm={async () => onChange(password)}
      onClose={onClose}
    >
      <Section
        level={3}
        footer={
          <>
            For <span className="font-semibold wrap-anywhere text-label">{network.ssid}</span>. It is saved with the
            list: press Save at the bottom of the page afterwards.
          </>
        }
      >
        <PasswordField
          label={passphrase ? 'New key passphrase' : 'New password'}
          labelNote="(leave empty to keep it)"
          placeholder="Unchanged"
          autoComplete="off"
          data-autofocus
          value={password}
          onChange={setPassword}
        />
      </Section>
    </Sheet>
  );
}
