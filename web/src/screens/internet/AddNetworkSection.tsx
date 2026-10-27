import { useId } from 'react';
import { useDevice } from '@/data/device';
import type { NearbyNetwork } from '@/data/types';
import { cn } from '@/lib/cn';
import { bandOfFrequency, formatDbm, signalLevel } from '@/lib/format';
import { standardLabel } from '../common/wifiOptions';
import { useAction } from '@/lib/useAction';
import { BreakableText, ButtonRow, FormError, Icon, Row, Section } from '@/ui';
import type { AddNetworkStart } from './AddNetworkSheet';
import { canJoin } from './networkDisplay';
import type { NetworkDraft } from './useNetworkDraft';

interface AddNetworkSectionProps {
  draft: NetworkDraft;
  wifiUplinkAvailable: boolean;
  connectedSsid: string;
  /** Opens the sheet that adds a network to the list. */
  onAdd: (start: AddNetworkStart) => void;
}

/** Ways to add a network: from a scan of the networks around, or by name. */
export function AddNetworkSection({ draft, wifiUplinkAvailable, connectedSsid, onAdd }: AddNetworkSectionProps) {
  const { nearbyNetworks, scanNearbyNetworks } = useDevice();
  // The scan's problems are shown in place: they can explain why a network is missing.
  const scanning = useAction(scanNearbyNetworks);
  const savedNames = new Set(draft.networks.map((network) => network.ssid));

  return (
    <Section title="Add a network">
      <ButtonRow
        leading={{ icon: 'wifi_find', className: 'text-tint' }}
        onClick={() => scanning.run()}
        busy={scanning.pending}
        busyLabel="Looking for networks…"
        disabled={!wifiUplinkAvailable}
      >
        Look for nearby networks
      </ButtonRow>
      {scanning.error && (
        <div className="row">
          <FormError>{scanning.error}</FormError>
        </div>
      )}
      {nearbyNetworks?.length === 0 && <Row title={<span className="text-label-2">No networks found.</span>} />}
      {nearbyNetworks?.map((network) => (
        <NearbyRow
          key={`${network.ssid}-${network.frequencyMhz}`}
          network={network}
          state={
            network.ssid === connectedSsid
              ? 'connected'
              : savedNames.has(network.ssid)
                ? 'saved'
                : canJoin(network)
                  ? 'joinable'
                  : 'unsupported'
          }
          onPick={() => canJoin(network) && onAdd({ from: 'scan', network })}
        />
      ))}
      <ButtonRow leading={{ icon: 'add', className: 'text-tint' }} onClick={() => onAdd({ from: 'manual' })}>
        Add manually
      </ButtonRow>
    </Section>
  );
}

const WIFI_BARS = {
  1: 'network_wifi_1_bar',
  2: 'network_wifi_2_bar',
  3: 'network_wifi_3_bar',
  4: 'signal_wifi_4_bar',
} as const;

interface NearbyRowProps {
  network: NearbyNetwork;
  /** In use, in the list already, can be added, or a kind RasBeacon can't join (WEP and the like). */
  state: 'connected' | 'saved' | 'joinable' | 'unsupported';
  onPick: () => void;
}

/**
 * A network found by the scan. When it can be added, its name is a button that covers the row,
 * named after the network alone, with the details as its description. Long names wrap after
 * "_" or "-", so similar names stay distinct.
 */
function NearbyRow({ network, state, onPick }: NearbyRowProps) {
  const detailsId = useId();
  const secured = network.security !== 'open';
  const note = { connected: 'Connected', saved: 'Saved', unsupported: 'Not supported', joinable: '' }[state];
  const details = [
    note,
    network.label,
    bandOfFrequency(network.frequencyMhz),
    network.standard && standardLabel(network.standard),
    formatDbm(network.signalDbm),
  ]
    .filter(Boolean)
    .join(' · ');
  const name = <BreakableText>{network.ssid}</BreakableText>;
  const nameClasses = cn(
    'block text-left text-body wrap-break-word',
    state === 'unsupported' ? 'text-label-2' : 'text-label',
  );

  return (
    <div
      className={cn(
        'row',
        state === 'joinable' &&
          'row-pressable has-[button:focus-visible]:outline-2 has-[button:focus-visible]:-outline-offset-2 has-[button:focus-visible]:outline-tint',
      )}
    >
      <span className="row-tile grid w-[1.8125rem] shrink-0 place-items-center lg:w-6">
        <Icon
          name={WIFI_BARS[signalLevel(network.signalDbm).bars]}
          size={22}
          className={cn('lg:size-[1.125rem]', state === 'connected' ? 'text-tint' : 'text-label-2')}
        />
      </span>
      <span className="min-w-0 flex-1">
        {state === 'joinable' ? (
          <button
            type="button"
            onClick={onPick}
            aria-describedby={detailsId}
            // The row draws the focus ring; the ::after layer makes the whole row the button.
            className={cn(nameClasses, 'after:absolute after:inset-0 focus-visible:outline-none')}
          >
            {name}
          </button>
        ) : (
          <span className={nameClasses}>{name}</span>
        )}
        <span id={detailsId} className="mt-0.5 block text-subhead text-label-2">
          {details}
        </span>
      </span>
      {state === 'connected' && <Icon name="check" size={20} className="text-tint" />}
      {/* As in iOS, only networks with a password carry a lock. */}
      {secured && <Icon name="lock" size={16} className="text-label-2" />}
      {state === 'joinable' && <Icon name="chevron_right" size={20} className="-mr-1 text-label-3 lg:size-4" />}
    </div>
  );
}
