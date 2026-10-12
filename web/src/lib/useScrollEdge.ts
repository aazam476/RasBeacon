import { useEffect, useState, type RefObject } from 'react';

/**
 * How the page sits under its floating bar, as in iOS: `underBar` once content has scrolled
 * under the bar (the bar shows its material then, transparent before), and `titleGone` once
 * the page's large title has passed under it (the bar shows the title in small instead).
 */
export function useScrollEdge(
  barRef: RefObject<HTMLElement | null>,
  titleRef: RefObject<HTMLElement | null>,
): { underBar: boolean; titleGone: boolean } {
  const [underBar, setUnderBar] = useState(false);
  const [titleGone, setTitleGone] = useState(false);

  useEffect(() => {
    // Only the boolean is kept, so scrolling re-renders the layout only when it flips.
    const update = () => setUnderBar(window.scrollY > 1);
    update();
    window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, []);

  useEffect(() => {
    const bar = barRef.current;
    const title = titleRef.current;
    if (!bar || !title) return;
    const barHeight = Math.round(bar.getBoundingClientRect().height);
    const observer = new IntersectionObserver(([entry]) => setTitleGone(!entry.isIntersecting), {
      rootMargin: `-${barHeight}px 0px 0px 0px`,
    });
    observer.observe(title);
    return () => observer.disconnect();
  }, [barRef, titleRef]);

  return { underBar, titleGone };
}
