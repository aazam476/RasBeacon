// Markup for the frame around the main pages: title and page navigation.

import type { ReactNode } from "react";
import type { Page } from "./useApp";

export interface LayoutViewProps {
  page: Page;
  setPage: (page: Page) => void;
  /** The current page's content. */
  children: ReactNode;
}

const PAGE_TITLES: Record<Page, string> = {
  status: "Status",
  internet: "Internet",
  vpn: "VPN",
  hotspot: "Hotspot",
  ssh: "SSH",
  https: "HTTPS",
  system: "System",
};

export function LayoutView({ page, setPage, children }: LayoutViewProps) {
  return (
    <>
      <header>
        <h1>RasBeacon</h1>
        <nav>
          {(Object.keys(PAGE_TITLES) as Page[]).map((p) => (
            <button
              key={p}
              type="button"
              aria-current={p === page ? "page" : undefined}
              onClick={() => setPage(p)}
            >
              {PAGE_TITLES[p]}
            </button>
          ))}
        </nav>
      </header>
      <main>{children}</main>
    </>
  );
}
