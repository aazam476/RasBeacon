import type { ReactNode } from 'react';
import { FALLBACK_LOGIN_URL } from '@/data/constants';
import { useDevice } from '@/data/device';
import type { PortalStatus } from '@/data/types';
import { formatCountdown } from '@/lib/format';
import { useAction } from '@/lib/useAction';
import { useCountdown } from '@/lib/useCountdown';
import { FullPage, FullPageHeader } from '@/shell/FullPage';
import { Loaded } from '@/shell/Loaded';
import {
  ActionButton,
  Badge,
  Button,
  buttonClasses,
  Disclosure,
  FormError,
  Icon,
  IconTile,
  Notice,
  Section,
  type BaseIconName,
  type TileColor,
} from '@/ui';
import { PortalBrowser } from './PortalBrowser';

/**
 * The hotel login page screen (`/#/portal`). Phones open it by themselves in their small "Log in
 * to network" sheet when the network RasBeacon is on wants a login, so it works without logging
 * in to RasBeacon. It offers the safe way first: logging in through RasBeacon's own browser.
 */
export function PortalScreen() {
  const { portal, reload } = useDevice();

  return (
    <FullPage title="Network login" width="lg">
      {/* The same heading in every state, "Nothing to do" included (the browser test looks for it). */}
      <FullPageHeader
        title="This network needs a login"
        // While RasBeacon's browser is open, the picture of the page gets the room.
        compact={portal.data?.browsing}
        icon={
          <span aria-hidden="true" className="grid size-[4.5rem] place-items-center rounded-full bg-tint/15 text-tint">
            <Icon name="login" size={40} />
          </span>
        }
      />
      <Loaded data={portal} what="the login page status" onRetry={() => reload('portal')}>
        {(status) =>
          status.browsing ? (
            <PortalBrowser />
          ) : status.loginMode ? (
            <LoginModeOn status={status} />
          ) : status.canStart || status.canBrowse ? (
            <WaysToLogIn status={status} />
          ) : (
            <NothingToDo detected={status.detected} />
          )
        }
      </Loaded>
    </FullPage>
  );
}

function WaysToLogIn({ status }: { status: PortalStatus }) {
  return (
    <>
      <p className="-mt-3 text-center text-body text-pretty text-label-2">
        The network RasBeacon is connected to (a hotel or café, for example) shows a login page before letting anyone
        online.
        {status.canStart &&
          " Your VPN's kill switch keeps your devices offline until then, so nobody can reach that page."}
      </p>

      {status.canBrowse && <ZeroLeakLogin />}

      {/* Login mode lets traffic past the VPN, so it stays tucked away while the safe way exists. */}
      {status.canStart &&
        (status.canBrowse ? (
          <Section>
            <Disclosure leading={{ icon: 'timer' }} title="Other way: login mode">
              <LoginModeOffer />
            </Disclosure>
          </Section>
        ) : (
          <OptionCard icon="timer" color="orange" title="Login mode">
            <LoginModeOffer />
          </OptionCard>
        ))}

      <CheckAgain note="(if you already logged in another way)" />
    </>
  );
}

interface OptionCardProps {
  icon: BaseIconName;
  color: TileColor;
  title: string;
  badge?: ReactNode;
  children: ReactNode;
}

/** One way to log in, as a group of its own. */
function OptionCard({ icon, color, title, badge, children }: OptionCardProps) {
  return (
    <section className="flex flex-col gap-4 rounded-group bg-cell p-5 lg:p-4">
      <div className="flex items-center gap-3">
        <IconTile icon={icon} color={color} size="large" />
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <h2 className="text-headline text-label">{title}</h2>
          {badge}
        </div>
      </div>
      {children}
    </section>
  );
}

function ZeroLeakLogin() {
  const { openPortalBrowser, portalBrowser } = useDevice();
  const opening = useAction(openPortalBrowser);
  const error = opening.error ?? portalBrowser.error;

  return (
    <OptionCard
      icon="verified_user"
      color="green"
      title="Log in through RasBeacon"
      badge={<Badge tone="green">Recommended</Badge>}
    >
      <p className="text-subhead text-label-2">
        RasBeacon opens the login page itself and shows it here. Nothing from your devices leaves the VPN.
      </p>
      <Button size="lg" icon="login" loading={opening.pending} onClick={() => opening.run()}>
        {opening.pending ? 'Opening…' : 'Open the login page here'}
      </Button>
      {error && <FormError>{error}</FormError>}
    </OptionCard>
  );
}

function LoginModeOffer() {
  const { startLoginMode } = useDevice();

  return (
    <>
      <p className="text-subhead text-label-2">
        Lets your devices reach the login page for 10 minutes. During that time, web and DNS traffic from all devices on
        the hotspot goes directly to this network,{' '}
        <strong className="font-semibold text-orange">not through the VPN</strong>. It ends by itself as soon as the
        login works.
      </p>
      <ActionButton
        variant="secondary"
        icon="timer"
        action={startLoginMode}
        busyLabel="Starting…"
        className="self-start"
      >
        Start login mode
      </ActionButton>
    </>
  );
}

function LoginModeOn({ status }: { status: PortalStatus }) {
  const { extendLoginMode, stopLoginMode } = useDevice();
  const secondsLeft = useCountdown(status.secondsLeft);
  const loginUrl = status.loginUrl || FALLBACK_LOGIN_URL;

  return (
    <>
      <OptionCard
        icon="timer"
        color="orange"
        title="Login mode is on"
        badge={
          <Badge tone="orange" pulse className="tabular">
            {formatCountdown(secondsLeft)} left
          </Badge>
        }
      >
        <p className="text-subhead text-label-2">
          Open the login page and log in (or accept the terms). RasBeacon notices within a few seconds and turns the VPN
          back on.
        </p>
        <a href={loginUrl} className={buttonClasses('primary', 'lg')}>
          <Icon name="open_in_new" size={20} />
          <span>Open the login page</span>
        </a>
        {/* Side by side once both labels fit on one line. */}
        <div className="grid gap-2.5 min-[480px]:grid-cols-2">
          <ActionButton variant="secondary" icon="add" action={extendLoginMode}>
            10 more minutes
          </ActionButton>
          <ActionButton variant="danger" icon="close" action={stopLoginMode}>
            Stop login mode
          </ActionButton>
        </div>
      </OptionCard>
      <Notice tone="warning">
        <p>
          While login mode is on, web and DNS traffic from all devices on the hotspot goes directly to this network, not
          through the VPN.
        </p>
      </Notice>
      <CheckAgain />
    </>
  );
}

function NothingToDo({ detected }: { detected: boolean }) {
  return (
    <>
      <Notice tone="success" icon="check_circle" title="Nothing to do">
        <p>
          {detected
            ? 'Your devices can reach the login page directly.'
            : "RasBeacon doesn't see a login page right now."}
        </p>
      </Notice>
      <div className="flex flex-wrap items-center justify-center gap-2.5">
        <a href="#/" className={buttonClasses('secondary')}>
          <Icon name="wifi_tethering" size={18} />
          <span>Open RasBeacon</span>
        </a>
        <CheckAgain />
      </div>
    </>
  );
}

/** "Check again": RasBeacon looks for a login page right away. */
function CheckAgain({ note }: { note?: string }) {
  const { checkPortal } = useDevice();

  return (
    <div className="flex flex-wrap items-center justify-center gap-x-3 gap-y-1">
      <ActionButton variant="secondary" icon="refresh" action={checkPortal} busyLabel="Checking…">
        Check again
      </ActionButton>
      {note && <span className="text-footnote text-label-2">{note}</span>}
    </div>
  );
}
