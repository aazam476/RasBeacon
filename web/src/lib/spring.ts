/**
 * Springs for motion that a finger can grab: the sheets. Following Apple's "Designing Fluid
 * Interfaces", a spring is described by two numbers instead of a duration:
 *
 * - `dampingRatio`: 1 settles without overshoot (the default for UI); below 1 it bounces a
 *   little, which only feels right after a flick.
 * - `response`: roughly how many seconds it takes to get there. Lower is snappier.
 *
 * A spring starts from where the element is now and at the speed it is moving, so an
 * animation can be grabbed or reversed at any moment without a jump. The motion is played
 * with the Web Animations API on `transform`, so it runs off the main thread.
 */

export interface SpringOptions {
  /** 1 = no overshoot (default). Use about 0.8 only when a flick preceded the motion. */
  dampingRatio?: number;
  /** Seconds, roughly the time to reach the target. Default 0.35. */
  response?: number;
  /** Starting speed in px/s (positive = moving down). Default 0. */
  velocity?: number;
}

interface RunningSpring {
  animation: Animation;
  from: number;
  to: number;
  options: Required<SpringOptions>;
  startedAt: number;
}

const running = new WeakMap<HTMLElement, RunningSpring>();

/** Displacement from the target and velocity of a spring `t` seconds after it started. */
function solve(x0: number, v0: number, dampingRatio: number, response: number, t: number): [number, number] {
  const w0 = (2 * Math.PI) / response;
  const zeta = dampingRatio;
  if (zeta < 1) {
    const wd = w0 * Math.sqrt(1 - zeta * zeta);
    const b = (v0 + zeta * w0 * x0) / wd;
    const decay = Math.exp(-zeta * w0 * t);
    const cos = Math.cos(wd * t);
    const sin = Math.sin(wd * t);
    const x = decay * (x0 * cos + b * sin);
    const v = decay * ((b * wd - zeta * w0 * x0) * cos - (x0 * wd + zeta * w0 * b) * sin);
    return [x, v];
  }
  if (zeta === 1) {
    const decay = Math.exp(-w0 * t);
    const c = v0 + w0 * x0;
    return [decay * (x0 + c * t), decay * (c - w0 * (x0 + c * t))];
  }
  const root = Math.sqrt(zeta * zeta - 1);
  const r1 = -w0 * (zeta - root);
  const r2 = -w0 * (zeta + root);
  const c2 = (v0 - r1 * x0) / (r2 - r1);
  const c1 = x0 - c2;
  return [c1 * Math.exp(r1 * t) + c2 * Math.exp(r2 * t), r1 * c1 * Math.exp(r1 * t) + r2 * c2 * Math.exp(r2 * t)];
}

/** Positions from `from` to `to` at 60 frames per second, until the spring is at rest. */
export function springValues(from: number, to: number, options: SpringOptions = {}): number[] {
  const { dampingRatio = 1, response = 0.35, velocity = 0 } = options;
  const x0 = from - to;
  const values = [from];
  for (let frame = 1; frame <= 180; frame++) {
    const [x, v] = solve(x0, velocity, dampingRatio, response, frame / 60);
    values.push(to + x);
    // At rest: within half a pixel and barely moving.
    if (Math.abs(x) < 0.5 && Math.abs(v) < 20) break;
  }
  values[values.length - 1] = to;
  return values;
}

/** The element's vertical offset as drawn right now, mid-animation included. */
export function currentY(element: HTMLElement): number {
  const transform = getComputedStyle(element).transform;
  if (!transform || transform === 'none') return 0;
  return new DOMMatrixReadOnly(transform).m42;
}

/** The speed (px/s) of the spring currently moving the element, or 0. */
function currentVelocity(element: HTMLElement): number {
  const spring = running.get(element);
  if (!spring || spring.animation.playState !== 'running') return 0;
  const { from, to, options, startedAt } = spring;
  const t = (performance.now() - startedAt) / 1000;
  return solve(from - to, options.velocity, options.dampingRatio, options.response, t)[1];
}

/**
 * Moves the element vertically to `to` (px) along a spring, starting from where it is drawn
 * right now and, unless `velocity` is given, at the speed it is already moving. Resolves when
 * it arrives; a newer call on the same element takes over smoothly.
 */
export function springY(element: HTMLElement, to: number, options: SpringOptions = {}): Promise<void> {
  const from = currentY(element);
  const velocity = options.velocity ?? currentVelocity(element);
  running.get(element)?.animation.cancel();

  const resolved = { dampingRatio: 1, response: 0.35, ...options, velocity };
  const values = springValues(from, to, resolved);
  element.style.transform = `translate3d(0, ${to}px, 0)`;
  const animation = element.animate(
    values.map((y) => ({ transform: `translate3d(0, ${y}px, 0)` })),
    { duration: ((values.length - 1) / 60) * 1000, easing: 'linear' },
  );
  running.set(element, { animation, from, to, options: resolved, startedAt: performance.now() });

  return animation.finished.then(
    () => undefined,
    // Cancelled by a newer animation: that one takes over.
    () => undefined,
  );
}

/** Stops any spring on the element and places it at `y` at once. */
export function setY(element: HTMLElement, y: number) {
  running.get(element)?.animation.cancel();
  running.delete(element);
  element.style.transform = `translate3d(0, ${y}px, 0)`;
}

/**
 * Where a flick would come to rest, like scrolling does: Apple's projection from the
 * "Designing Fluid Interfaces" sample code. `velocity` in px/s.
 */
export function project(velocity: number, decelerationRate = 0.998): number {
  return ((velocity / 1000) * decelerationRate) / (1 - decelerationRate);
}

/** Resistance past an edge: the further past it, the less the element follows the finger. */
export function rubberband(overshoot: number, dimension: number, constant = 0.55): number {
  return (overshoot * dimension * constant) / (dimension + constant * Math.abs(overshoot));
}

/** True when the person asked their device for less motion. */
export function prefersReducedMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}
