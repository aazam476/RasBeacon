import { useRef } from 'react';
import { Link, Outlet, useLocation } from 'react-router';
import { cn } from '@/lib/cn';
import { useScrollEdge } from '@/lib/useScrollEdge';
import { Icon } from '@/ui';
import { isSubPage, useCurrentPage, usePageTitleOverride } from './navigation';
import { Sidebar } from './Sidebar';

/**
 * Computers (1024px and wider), laid out like macOS System Settings: the sidebar, and the page
 * in a column up to 704px wide with its title (wider for Status and Clients, which lay their
 * sections out in two columns). Once the title scrolls away, a translucent bar holds it.
 */
export function DesktopLayout() {
  const page = useCurrentPage();
  const { pathname } = useLocation();
  const sub = isSubPage(pathname);
  const override = usePageTitleOverride();
  const title = (sub && override) || page.label;
  const barRef = useRef<HTMLDivElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const { underBar, titleGone } = useScrollEdge(barRef, titleRef);

  return (
    <div className="min-h-dvh">
      <Sidebar />
      {/*
        The bar only shows the title once it has scrolled away; it holds nothing to press, so
        presses go through it (to a sub-page's back link right under it, for one).
      */}
      <div ref={barRef} className="pointer-events-none fixed top-0 right-0 left-64 z-20 h-12">
        <div
          aria-hidden="true"
          className={cn(
            'scroll-edge absolute inset-x-0 top-0 h-[calc(100%+1.25rem)] transition-opacity duration-200',
            underBar ? 'opacity-100' : 'opacity-0',
          )}
        />
        <p
          aria-hidden="true"
          className={cn(
            'relative mx-auto box-content px-8 pt-3.5 text-headline text-label transition-opacity duration-150',
            page.wide ? 'max-w-[72rem]' : 'max-w-[44rem]',
            titleGone ? 'opacity-100' : 'opacity-0',
          )}
        >
          {title}
        </p>
      </div>
      <main className="pl-64">
        <div className={cn('mx-auto box-content px-8 pt-10 pb-16', page.wide ? 'max-w-[72rem]' : 'max-w-[44rem]')}>
          {sub && (
            // A sub-page (a plugin) leads back to its page.
            <Link
              to={page.path}
              className="pressable -ml-1.5 mb-2 inline-flex items-center text-body text-tint hover:underline"
            >
              <Icon name="chevron_left" size={22} />
              {page.label}
            </Link>
          )}
          <h1 ref={titleRef} className="pb-5 text-large-title wrap-break-word text-label">
            {title}
          </h1>
          <div className="flex flex-col gap-7">
            <Outlet />
          </div>
        </div>
      </main>
    </div>
  );
}
