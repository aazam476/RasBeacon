import { useDevice } from '@/data/device';
import type { PortalStatus } from '@/data/types';
import { ActionButton, Notice } from '@/ui';
import { LoginPageLink } from './LoginPageLink';

/**
 * Shown at the top of Status and Internet while the network RasBeacon is on wants a login
 * (a hotel or café page), with the way to it.
 */
export function PortalNotice({ portal }: { portal: PortalStatus }) {
  const { checkPortal } = useDevice();
  const blocked = portal.canStart || portal.canBrowse;

  return (
    <Notice
      tone="warning"
      icon="login"
      title="This network shows a login page"
      actions={
        <>
          <LoginPageLink portal={portal} />
          <ActionButton variant="secondary" size="sm" icon="refresh" action={checkPortal} busyLabel="Checking…">
            Check again
          </ActionButton>
        </>
      }
    >
      <p>
        {blocked
          ? "Your devices can't reach it while the VPN keeps them offline, so log in through RasBeacon."
          : 'Open it to log in; your devices can reach it directly.'}
      </p>
    </Notice>
  );
}
