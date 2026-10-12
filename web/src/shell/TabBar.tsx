import { Link, useLocation } from 'react-router';
import { Icon } from '@/ui';
import { isUnderMore, MORE_ITEM, TAB_ITEMS, type NavItem } from './navigation';

/**
 * The phone's tab bar: a glass capsule floating over the bottom of the page, within reach of
 * the thumb. Four pages and More, which lists the rest and stays selected on them.
 *
 * The selected tab sits on a pill in the tint color. The pill is a second, opaque copy of the
 * tabs, clipped to the selected one; moving the clip slides the pill, and the colors change in
 * step with it because they are one layer.
 */
export function TabBar() {
  const { pathname } = useLocation();
  const selected = TAB_ITEMS.findIndex((item) => (item === MORE_ITEM ? isUnderMore(pathname) : item.path === pathname));
  const count = TAB_ITEMS.length;

  return (
    <nav
      aria-label="Main navigation"
      className="pointer-events-none fixed inset-x-0 bottom-0 z-40 px-4 pb-[max(env(safe-area-inset-bottom),0.75rem)]"
    >
      <div className="glass pointer-events-auto relative mx-auto max-w-[27rem] rounded-full p-1">
        <ul className="grid" style={{ gridTemplateColumns: `repeat(${count}, minmax(0, 1fr))` }}>
          {TAB_ITEMS.map((item, index) => (
            <li key={item.path} className="flex">
              <Link
                to={item.path}
                aria-current={index === selected ? 'page' : undefined}
                className="pressable flex flex-1 flex-col items-center gap-0.5 rounded-full pt-1.5 pb-1 text-label"
              >
                <Tab item={item} />
              </Link>
            </li>
          ))}
        </ul>
        {selected >= 0 && (
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-1 grid rounded-full bg-[color-mix(in_srgb,var(--c-tint)_12%,var(--c-raised))] text-tint transition-[clip-path] duration-250 ease-in-out motion-reduce:transition-none"
            style={{
              gridTemplateColumns: `repeat(${count}, minmax(0, 1fr))`,
              clipPath: `inset(0 ${((count - 1 - selected) * 100) / count}% 0 ${(selected * 100) / count}% round 999px)`,
            }}
          >
            {TAB_ITEMS.map((item) => (
              <span key={item.path} className="flex flex-col items-center gap-0.5 pt-1.5 pb-1">
                <Tab item={item} selected />
              </span>
            ))}
          </div>
        )}
      </div>
    </nav>
  );
}

function Tab({ item, selected = false }: { item: NavItem; selected?: boolean }) {
  return (
    <>
      <Icon name={item.icon} filled={selected} size={24} />
      <span className="text-[0.6875rem] leading-[0.875rem] font-semibold tracking-[0.005em]">{item.label}</span>
    </>
  );
}
