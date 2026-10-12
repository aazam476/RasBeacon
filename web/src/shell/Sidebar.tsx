import { NavLink } from 'react-router';
import { useDevice } from '@/data/device';
import { cn } from '@/lib/cn';
import { AppIcon } from '@/ui/AppIcon';
import { IconTile } from '@/ui';
import { LogoutButton } from './LogoutButton';
import { NAV_GROUPS } from './navigation';

/**
 * The desktop sidebar, as in macOS System Settings: a glass panel floating at the left edge,
 * with the pages in groups, each on its colored tile, and logging out at the foot.
 */
export function Sidebar() {
  const { status } = useDevice();
  const device = status.data?.device.hostname;

  return (
    <aside className="glass fixed inset-y-2 left-2 z-30 flex w-60 flex-col rounded-[18px]">
      <div className="flex items-center gap-2.5 px-4 pt-4 pb-3">
        <AppIcon />
        <div className="min-w-0">
          <p className="text-headline text-label">RasBeacon</p>
          {device && (
            <p className="truncate text-footnote text-label-2" title={device}>
              {device}
            </p>
          )}
        </div>
      </div>

      <nav aria-label="Main navigation" className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-2.5 pt-1 pb-3">
        {NAV_GROUPS.map((group, index) => (
          <div key={group.heading ?? index} className="flex flex-col gap-px">
            {group.heading && <p className="px-2 pb-1 text-footnote font-semibold text-label-2">{group.heading}</p>}
            {group.items.map((item) => (
              <NavLink
                key={item.path}
                to={item.path}
                end
                className={({ isActive }) =>
                  cn(
                    'flex min-h-8 items-center gap-2.5 rounded-[8px] px-2 text-body text-label transition-colors',
                    isActive ? 'bg-fill font-semibold' : 'hover:bg-fill-2 active:bg-fill',
                  )
                }
              >
                <IconTile icon={item.icon} color={item.tile} />
                <span className="truncate">{item.label}</span>
              </NavLink>
            ))}
          </div>
        ))}
      </nav>

      <div className="border-t border-separator px-2.5 py-2">
        <LogoutButton look="sidebar" />
      </div>
    </aside>
  );
}
