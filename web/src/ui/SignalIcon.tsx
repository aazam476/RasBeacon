import { signalLevel } from '@/lib/format';
import { Icon } from './Icon';

const BARS = {
  1: 'network_wifi_1_bar',
  2: 'network_wifi_2_bar',
  3: 'network_wifi_3_bar',
  4: 'signal_wifi_4_bar',
} as const;

/** Wi-Fi signal strength as the familiar fan of bars, with its quality as the accessible name. */
export function SignalIcon({ dbm, className }: { dbm: number; className?: string }) {
  const level = signalLevel(dbm);

  return (
    <span role="img" aria-label={`${level.quality} signal`} title={`${level.quality} signal`} className={className}>
      <Icon name={BARS[level.bars]} size={18} className="text-label-2" />
    </span>
  );
}
