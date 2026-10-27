import { Link } from 'react-router';
import { useDevice } from '@/data/device';
import { buttonClasses, Notice } from '@/ui';

/**
 * Why priority devices don't go first yet, when they don't: the internet speed isn't set, or
 * RasBeacon couldn't set up its queues. Nothing while it works (or with no priority devices).
 */
export function PriorityNotice() {
  const { qosSettings } = useDevice();
  const qos = qosSettings.data;
  if (!qos || qos.priorityDevices === 0) return null;
  if (qos.problem) {
    return (
      <Notice tone="warning" title="Priority devices don’t go first">
        <p>{qos.problem}</p>
      </Notice>
    );
  }
  if (qos.uploadKbps > 0 || qos.downloadKbps > 0) return null;
  return (
    <Notice
      title="Set your internet speed"
      actions={
        <Link to="/internet" className={buttonClasses('secondary', 'sm')}>
          Open Internet settings
        </Link>
      }
    >
      <p>Priority devices go first once RasBeacon knows how fast the internet connection is.</p>
    </Notice>
  );
}
