import { useRef } from 'react';
import { Link, Outlet } from 'react-router';
import { cn } from '@/lib/cn';
import { useScrollEdge } from '@/lib/useScrollEdge';
import { Icon } from '@/ui';
import { LogoutButton } from './LogoutButton';
import { MORE_ITEM, MORE_ITEMS, useCurrentPage } from './navigation';
import { TabBar } from './TabBar';

/**
 * Phones and tablets (below 1024px), laid out like iOS Settings: a large title that scrolls
 * away under a translucent bar, inset groups of rows in one column (at most 640px wide), and
 * the floating tab bar.
 */
export function PhoneLayout() {
  const page = useCurrentPage();
  const barRef = useRef<HTMLElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const { underBar, titleGone } = useScrollEdge(barRef, titleRef);
  // Pages listed under More have no tab of their own, so their bar leads back to it.
  const underMore = MORE_ITEMS.includes(page);

  return (
    <div className="min-h-dvh">
      <header ref={barRef} className="fixed inset-x-0 top-0 z-40 pt-[env(safe-area-inset-top)]">
        <div
          aria-hidden="true"
          className={cn(
            'scroll-edge absolute inset-x-0 top-0 h-[calc(100%+1.25rem)] transition-opacity duration-200',
            underBar ? 'opacity-100' : 'opacity-0',
          )}
        />
        <div className="relative mx-auto flex h-13 max-w-[40rem] items-center gap-2 px-4">
          {underMore && (
            <Link
              to={MORE_ITEM.path}
              aria-label={`Back to ${MORE_ITEM.label}`}
              className="glass pressable flex h-9 items-center rounded-full pr-3.5 pl-1.5 text-body text-label"
            >
              <Icon name="chevron_left" size={24} />
              {MORE_ITEM.label}
            </Link>
          )}
          {/* The title moves up here once the large one has scrolled away. */}
          <p
            aria-hidden="true"
            className={cn(
              'pointer-events-none absolute inset-x-28 truncate text-center text-headline text-label transition-opacity duration-150',
              titleGone ? 'opacity-100' : 'opacity-0',
            )}
          >
            {page.label}
          </p>
          <div className="ml-auto">
            <LogoutButton look="bar" />
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-[40rem] px-4 pt-[calc(env(safe-area-inset-top)+3.25rem)] pb-[calc(max(env(safe-area-inset-bottom),0.75rem)+6rem)]">
        <h1 ref={titleRef} className="pt-1 pb-4 text-large-title text-label">
          {page.label}
        </h1>
        <div className="flex flex-col gap-7">
          <Outlet />
        </div>
      </main>

      {/* Content fades out under the floating tab bar instead of meeting its edge. */}
      <div
        aria-hidden="true"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-30 h-[calc(max(env(safe-area-inset-bottom),0.75rem)+6rem)] bg-linear-to-t from-bg from-35% to-transparent"
      />
      <TabBar />
    </div>
  );
}
