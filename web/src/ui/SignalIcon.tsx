import { signalLevel } from '@/lib/format';
import { Icon } from './Icon';
import { SIGNAL_BARS } from './icons';

/** Wi-Fi signal strength as the familiar fan of bars, with its quality as the accessible name. */
export function SignalIcon({ dbm, className }: { dbm: number; className?: string }) {
  const level = signalLevel(dbm);

  return (
    <span role="img" aria-label={`${level.quality} signal`} title={`${level.quality} signal`} className={className}>
      <Icon name={SIGNAL_BARS[level.bars]} size={18} className="text-label-2" />
    </span>
  );
}
