// Pure helpers for the zero-leak login screen (see usePortalBrowser.ts):
// turning a finger or mouse on the picture into a tap or a scroll on the
// real page in RasBeacon's browser.

/** Where a pointer went down or up, in screen pixels. */
export interface PointerSpot {
  clientX: number;
  clientY: number;
}

/** The picture's position and size on screen (from getBoundingClientRect). */
export interface PictureBox {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** The page's size in RasBeacon's browser, in CSS pixels. */
export interface PageSize {
  width: number;
  height: number;
}

/**
 * Converts a spot on the picture to the same spot on the page. The picture
 * is usually shown smaller or larger than the page, so it is scaled.
 */
export function toPagePoint(
  spot: PointerSpot,
  box: PictureBox,
  page: PageSize,
): { x: number; y: number } {
  const scaleX = box.width > 0 ? page.width / box.width : 1;
  const scaleY = box.height > 0 ? page.height / box.height : 1;
  const clamp = (v: number, high: number) => Math.round(Math.max(0, Math.min(high, v)));
  return {
    x: clamp((spot.clientX - box.left) * scaleX, page.width),
    y: clamp((spot.clientY - box.top) * scaleY, page.height),
  };
}

/** A finger that moved less than this (screen pixels) tapped; more, it swiped. */
export const TAP_SLOP = 10;

export type Gesture =
  | { kind: "click"; x: number; y: number }
  | { kind: "scroll"; x: number; y: number; deltaY: number };

/**
 * Decides what a pointer going down at `start` and up at `end` meant: a tap
 * (click there) or a swipe (scroll the page: swiping up scrolls down, as on
 * any phone).
 */
export function classifyGesture(
  start: PointerSpot,
  end: PointerSpot,
  box: PictureBox,
  page: PageSize,
): Gesture {
  const at = toPagePoint(start, box, page);
  const movedY = end.clientY - start.clientY;
  const movedX = end.clientX - start.clientX;
  if (Math.abs(movedY) < TAP_SLOP && Math.abs(movedX) < TAP_SLOP) {
    return { kind: "click", ...at };
  }
  const scaleY = box.height > 0 ? page.height / box.height : 1;
  return { kind: "scroll", ...at, deltaY: Math.round(-movedY * scaleY) };
}

/**
 * The page size to ask for: this screen's width, and most of its height
 * (the controls below the picture need the rest).
 */
export function pageSizeFor(screenWidth: number, screenHeight: number): PageSize {
  return { width: Math.round(screenWidth), height: Math.round(screenHeight * 0.7) };
}
