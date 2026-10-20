import { useDevice } from '@/data/device';
import { FullPage, FullPageHeader } from '@/shell/FullPage';
import { ActionButton, ActivityIndicator, AppIcon, Icon, type BaseIconName } from '@/ui';

export type NoticeKind = 'unreachable' | 'setup-closed' | 'setup-error' | 'no-radio' | 'off';

const NOTICES: Record<
  NoticeKind,
  { title: string; body: string; icon: BaseIconName; tone: 'gray' | 'orange' | 'red' }
> = {
  unreachable: {
    title: "Can't reach RasBeacon",
    body: 'Check that this device is connected to the RasBeacon hotspot, then try again.',
    icon: 'link_off',
    tone: 'orange',
  },
  'setup-closed': {
    title: 'Setup window closed',
    body: 'Nobody finished setup within 30 minutes, so the setup network was turned off. Unplug RasBeacon and plug it back in to reopen it.',
    icon: 'timer',
    tone: 'orange',
  },
  'setup-error': {
    title: "setup.txt can't be used",
    body: "The setup.txt file on the SD card has a problem, so the setup network stays off for safety. Fix or delete the file (see the README's troubleshooting section) and restart RasBeacon.",
    icon: 'sd_card',
    tone: 'red',
  },
  off: {
    title: 'RasBeacon is off',
    body: 'Unplug the power and plug it back in to turn it on again. Then join its Wi-Fi and try again.',
    icon: 'power_settings_new',
    tone: 'gray',
  },
  'no-radio': {
    title: 'No Wi-Fi adapter found',
    body: 'RasBeacon found no Wi-Fi radio that can run a hotspot. Plug in a supported USB Wi-Fi adapter and restart.',
    icon: 'wifi_off',
    tone: 'red',
  },
};

const TONES = {
  gray: 'bg-fill text-label-2',
  orange: 'bg-orange-fill/15 text-orange',
  red: 'bg-red-fill/15 text-red',
};

/** A full-page message when there is nothing to set up from here, with "Try again". */
export function NoticeScreen({ kind, detail }: { kind: NoticeKind; detail?: string }) {
  const { retry } = useDevice();
  const notice = NOTICES[kind];

  return (
    <FullPage title={notice.title} centered>
      <FullPageHeader
        title={notice.title}
        icon={
          <span
            aria-hidden="true"
            className={`grid size-[4.5rem] place-items-center rounded-full ${TONES[notice.tone]}`}
          >
            <Icon name={notice.icon} filled size={40} />
          </span>
        }
      >
        <p>{notice.body}</p>
        {detail && <p className="selectable font-mono text-footnote wrap-anywhere">Details: {detail}</p>}
      </FullPageHeader>
      <ActionButton size="lg" icon="refresh" action={retry}>
        Try again
      </ActionButton>
    </FullPage>
  );
}

/** Shown while the app finds out whether RasBeacon is set up and whether you're logged in. */
export function LoadingScreen() {
  return (
    <FullPage title="Loading" centered>
      <div className="flex flex-col items-center gap-5">
        <AppIcon size="lg" />
        <div role="status" className="flex items-center gap-2 text-label-2">
          <ActivityIndicator size={20} />
          <h1 className="text-body">Loading…</h1>
        </div>
      </div>
    </FullPage>
  );
}
