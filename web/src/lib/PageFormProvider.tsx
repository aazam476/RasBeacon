import { useCallback, useMemo, useRef, useState, type ReactNode } from 'react';
import { PageFormContext, type PagePart } from './pageForm';

/** The page around the sections; the bar inside it reads their state with usePageForm. */
export function PageFormProvider({ children }: { children: ReactNode }) {
  const parts = useRef(new Map<string, PagePart>());
  const [version, setVersion] = useState(0);

  const set = useCallback((id: string, part: PagePart) => {
    const old = parts.current.get(id);
    parts.current.set(id, part);
    // Only what the bar shows makes it redraw (a new submit function every render doesn't).
    if (
      !old ||
      old.dirty !== part.dirty ||
      old.pending !== part.pending ||
      old.error !== part.error ||
      old.invalid !== part.invalid
    ) {
      setVersion((n) => n + 1);
    }
  }, []);
  const remove = useCallback((id: string) => {
    if (parts.current.delete(id)) setVersion((n) => n + 1);
  }, []);
  const list = useCallback(() => [...parts.current].sort(([, a], [, b]) => a.order - b.order), []);

  const registry = useMemo(() => ({ set, remove, parts: list, version }), [set, remove, list, version]);
  return <PageFormContext.Provider value={registry}>{children}</PageFormContext.Provider>;
}
