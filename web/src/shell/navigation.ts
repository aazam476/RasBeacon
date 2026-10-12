import { useEffect } from 'react';
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
  { heading: 'Device', items: [SSH, HTTPS, SYSTEM] },
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
export const MORE_ITEMS: readonly NavItem[] = [HOTSPOT, GUESTS, SSH, HTTPS, SYSTEM];

const ALL_ITEMS = [...NAV_GROUPS.flatMap((group) => group.items), MORE_ITEM];

/**
 * The page being shown. It also puts the page's name in the browser tab and scrolls back to the
 * top when the user switches pages.
 */
export function useCurrentPage(): NavItem {
  const { pathname } = useLocation();
  const page = ALL_ITEMS.find((item) => item.path === pathname) ?? STATUS;

  useDocumentTitle(page.label);

  useEffect(() => {
    window.scrollTo(0, 0);
  }, [pathname]);

  return page;
}

/** True on the More page and the pages it lists, where the More tab stays selected. */
export function isUnderMore(pathname: string): boolean {
  return pathname === MORE_ITEM.path || MORE_ITEMS.some((item) => item.path === pathname);
}
