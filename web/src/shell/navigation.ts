import { useEffect, useSyncExternalStore } from 'react';
import { useLocation } from 'react-router';
import { useDocumentTitle } from '@/lib/useDocumentTitle';
import type { BaseIconName, TileColor } from '@/ui';

export interface NavItem {
  path: string;
  label: string;
  /** The symbol in the tab bar, and on the page's colored tile in the sidebar and on More. */
  icon: BaseIconName;
  tile: TileColor;
  /** On computers the page uses a wider column, for two columns of sections. */
  wide?: boolean;
}

interface NavGroup {
  /** Heading in the desktop sidebar; the first group has none. */
  heading?: string;
  items: NavItem[];
}

const STATUS: NavItem = { path: '/', label: 'Status', icon: 'dashboard', tile: 'indigo', wide: true };
const INTERNET: NavItem = {
  path: '/internet',
  label: 'Internet',
  icon: 'public',
  tile: 'blue',
};
const VPN: NavItem = {
  path: '/vpn',
  label: 'VPN',
  icon: 'vpn_lock',
  tile: 'teal',
};
const HOTSPOT: NavItem = {
  path: '/hotspot',
  label: 'Hotspot',
  icon: 'wifi',
  tile: 'green',
};
const GUESTS: NavItem = {
  path: '/guests',
  label: 'Guests',
  icon: 'group',
  tile: 'orange',
};
const CLIENTS: NavItem = {
  path: '/clients',
  label: 'Clients',
  icon: 'devices',
  tile: 'purple',
  wide: true,
};
const SSH: NavItem = {
  path: '/ssh',
  label: 'SSH',
  icon: 'terminal',
  tile: 'gray',
};
const HTTPS: NavItem = {
  path: '/https',
  label: 'HTTPS',
  icon: 'lock',
  tile: 'blue',
};
const PLUGINS: NavItem = {
  path: '/plugins',
  label: 'Plugins',
  icon: 'extension',
  tile: 'indigo',
};
const SYSTEM: NavItem = {
  path: '/system',
  label: 'System',
  icon: 'settings',
  tile: 'gray',
};

/** Every page, grouped as in the desktop sidebar. */
export const NAV_GROUPS: readonly NavGroup[] = [
  { items: [STATUS] },
  { heading: 'Connection', items: [INTERNET, VPN] },
  { heading: 'Wi-Fi', items: [HOTSPOT, GUESTS, CLIENTS] },
  { heading: 'Device', items: [PLUGINS, SSH, HTTPS, SYSTEM] },
];

/** The phone's More page and its tab. */
export const MORE_ITEM: NavItem = {
  path: '/more',
  label: 'More',
  icon: 'more_horiz',
  tile: 'gray',
};

/** The pages on the phone's tab bar, More last. The rest are on the More page. */
export const TAB_ITEMS: readonly NavItem[] = [STATUS, INTERNET, VPN, CLIENTS, MORE_ITEM];

/** The pages listed on the More page. */
export const MORE_ITEMS: readonly NavItem[] = [HOTSPOT, GUESTS, PLUGINS, SSH, HTTPS, SYSTEM];

const ALL_ITEMS = [...NAV_GROUPS.flatMap((group) => group.items), MORE_ITEM];

/** The page an address belongs to: its own, or the one it is a sub-page of (/plugins/x). */
function pageOf(pathname: string): NavItem | undefined {
  return (
    ALL_ITEMS.find((item) => item.path === pathname) ??
    ALL_ITEMS.find((item) => item.path !== '/' && pathname.startsWith(`${item.path}/`))
  );
}

/** True on a sub-page (/plugins/x), whose bar leads back to its page. */
export function isSubPage(pathname: string): boolean {
  return pageOf(pathname)?.path !== pathname;
}

// A sub-page's own title (a plugin's name), set by the screen with usePageTitle.
let titleOverride: string | undefined;
const titleListeners = new Set<() => void>();
function subscribeTitle(listener: () => void) {
  titleListeners.add(listener);
  return () => titleListeners.delete(listener);
}

/** Gives a sub-page its own title while it is shown. */
export function usePageTitle(title: string | undefined) {
  useEffect(() => {
    titleOverride = title;
    titleListeners.forEach((listener) => listener());
    return () => {
      titleOverride = undefined;
      titleListeners.forEach((listener) => listener());
    };
  }, [title]);
}

/** The title a sub-page set, if any. */
export function usePageTitleOverride(): string | undefined {
  return useSyncExternalStore(subscribeTitle, () => titleOverride);
}

/**
 * The page being shown (for a sub-page, the page it belongs to). It also puts the page's name in
 * the browser tab and scrolls back to the top when the user switches pages.
 */
export function useCurrentPage(): NavItem {
  const { pathname } = useLocation();
  const page = pageOf(pathname) ?? STATUS;
  const override = usePageTitleOverride();

  useDocumentTitle(override ?? page.label);

  useEffect(() => {
    window.scrollTo(0, 0);
  }, [pathname]);

  return page;
}

/** True on the More page and the pages it lists, where the More tab stays selected. */
export function isUnderMore(pathname: string): boolean {
  const page = pageOf(pathname);
  return pathname === MORE_ITEM.path || (page !== undefined && MORE_ITEMS.includes(page));
}
