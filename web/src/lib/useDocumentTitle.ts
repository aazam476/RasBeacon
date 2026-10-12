import { useEffect } from 'react';

/** Shows "<page> · RasBeacon" in the browser tab while the component is on screen. */
export function useDocumentTitle(page: string) {
  useEffect(() => {
    document.title = `${page} · RasBeacon`;
  }, [page]);
}
