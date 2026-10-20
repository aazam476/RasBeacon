// Runs before each component test file. jsdom (the simulated browser) has no layout, animation
// or modal dialogs, so this file adds small stand-ins for the few the app uses, and lets tests
// pick a screen size.

import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach } from 'vitest';

/** The window width matchMedia answers for: a phone unless a test says otherwise. */
let viewportWidth = 390;

/**
 * Shows the screen as on a phone (390 px, tab bar) or a computer (1280 px, sidebar). Call it
 * before rendering; layouts switch at 64rem (DESKTOP_QUERY in src/lib/useMediaQuery.ts).
 */
export function setViewport(size: 'phone' | 'desktop') {
  viewportWidth = size === 'phone' ? 390 : 1280;
}

/** A width in a media query, in pixels (1rem = 16px). */
function pixels(value: string, unit: string): number {
  return unit === 'rem' || unit === 'em' ? Number(value) * 16 : Number(value);
}

/** Answers min-width and max-width queries from viewportWidth; anything else is false. */
function matches(query: string): boolean {
  const min = /min-width:\s*([\d.]+)(px|rem|em)/.exec(query);
  const max = /max-width:\s*([\d.]+)(px|rem|em)/.exec(query);
  if (!min && !max) return false;
  return (!min || viewportWidth >= pixels(min[1], min[2])) && (!max || viewportWidth <= pixels(max[1], max[2]));
}

window.matchMedia = (query: string) =>
  ({
    matches: matches(query),
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  }) as MediaQueryList;

// Modal dialogs: open and close them, as the browser would.
HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
  this.setAttribute('open', '');
};
HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
  this.removeAttribute('open');
  this.dispatchEvent(new Event('close'));
};

// Animations finish at once.
Element.prototype.animate = function animate() {
  return { finished: Promise.resolve(), cancel: () => undefined } as unknown as Animation;
};
Element.prototype.getAnimations = () => [];

// Where an element is drawn: the vertical offset of the translate3d(…) the springs set (jsdom
// keeps the style but has no layout).
window.DOMMatrixReadOnly = class {
  m42: number;
  constructor(transform = '') {
    this.m42 = Number(/translate3d\([^,]+,\s*(-?[\d.]+)px/.exec(transform)?.[1] ?? 0);
  }
} as unknown as typeof DOMMatrixReadOnly;

window.scrollTo = () => undefined;
// Pointer capture (the login page picture keeps following a finger that leaves it).
Element.prototype.setPointerCapture = () => undefined;
Element.prototype.releasePointerCapture = () => undefined;
window.IntersectionObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
} as unknown as typeof IntersectionObserver;

beforeEach(() => setViewport('phone'));
// Empty the page after every test, so tests don't see each other's screens.
afterEach(() => cleanup());
