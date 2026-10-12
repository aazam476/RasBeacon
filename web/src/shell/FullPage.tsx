import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { useDocumentTitle } from '@/lib/useDocumentTitle';
import { AppIcon } from '@/ui/AppIcon';

interface FullPageProps {
  /** For the browser tab. */
  title: string;
  /** Centers short content (login, notices) vertically; long content starts at the top. */
  centered?: boolean;
  /** `md` for short forms and messages, `lg` for setup and the login page picture. */
  width?: 'md' | 'lg';
  children: ReactNode;
}

/**
 * A screen without navigation: notices, first-time setup, login and the hotel login page. The
 * same on phones and computers, like a setup screen on iOS.
 */
export function FullPage({ title, centered = false, width = 'md', children }: FullPageProps) {
  useDocumentTitle(title);

  return (
    <main
      className={cn(
        'mx-auto flex min-h-dvh w-full flex-col gap-7 px-4 pt-[calc(env(safe-area-inset-top)+3rem)] pb-[calc(env(safe-area-inset-bottom)+2.5rem)] lg:pt-20',
        centered && 'justify-center lg:pt-10',
        width === 'md' ? 'max-w-[26rem]' : 'max-w-[36rem]',
      )}
    >
      {children}
    </main>
  );
}

interface FullPageHeaderProps {
  title: string;
  /** Replaces the app icon, e.g. with a state symbol. */
  icon?: ReactNode;
  /** Just the title, smaller, for screens whose content needs the room (the login page picture). */
  compact?: boolean;
  /** A line or two under the title. */
  children?: ReactNode;
}

/** The top of a full page: an icon, the large title (the page's h1) and what it is about. */
export function FullPageHeader({ title, icon, compact = false, children }: FullPageHeaderProps) {
  if (compact) {
    return (
      // Clear of the demo's tag in the top corner, when there is one.
      <header className="-mt-6 pr-(--demo-tag) lg:-mt-10 lg:pr-0">
        <h1 className="text-title3 text-label">{title}</h1>
      </header>
    );
  }

  return (
    <header className="flex flex-col items-center gap-3 text-center">
      {icon ?? <AppIcon size="lg" />}
      <h1 className="mt-1 text-large-title text-balance text-label">{title}</h1>
      {children && <div className="flex flex-col gap-2 text-body text-pretty text-label-2">{children}</div>}
    </header>
  );
}
