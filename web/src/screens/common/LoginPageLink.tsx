import { Link } from 'react-router';
import { loginPageAddress } from '@/data/constants';
import type { PortalStatus } from '@/data/types';
import { cn } from '@/lib/cn';
import { formatCountdown } from '@/lib/format';
import { useCountdown } from '@/lib/useCountdown';
import { buttonClasses, Icon } from '@/ui';

/**
 * The way to the hotel's login page from the Status and Internet pages. While the VPN keeps
 * devices offline, this device can't open the page itself, so the link goes to the login page
 * screen (`/#/portal`), which says what is going on there. Only when devices can reach the page
 * directly does it open the page.
 */
export function LoginPageLink({ portal, className }: { portal: PortalStatus; className?: string }) {
  const secondsLeft = useCountdown(portal.secondsLeft);
  const classes = cn(buttonClasses('primary', 'sm'), className);

  if (portal.loginMode || portal.browsing || portal.canStart || portal.canBrowse) {
    const label = portal.loginMode
      ? `Login mode is on (${formatCountdown(secondsLeft)} left)`
      : portal.browsing
        ? "RasBeacon's browser is open on the login page"
        : 'Log in to it';
    return (
      <Link to="/portal" className={classes}>
        <Icon name="login" size={16} />
        <span>{label}</span>
      </Link>
    );
  }

  return (
    <a href={loginPageAddress(portal.loginUrl)} target="_blank" rel="noreferrer" className={classes}>
      <Icon name="open_in_new" size={16} />
      <span>Open the login page</span>
    </a>
  );
}
