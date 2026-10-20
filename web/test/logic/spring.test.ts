// The springs that move the sheets (src/lib/spring.ts): where they go and how they feel.
import { describe, expect, it } from 'vitest';
import { project, rubberband, springValues } from '@/lib/spring';

describe('springValues', () => {
  it('starts where it is and ends exactly at the target', () => {
    const values = springValues(300, 0);
    expect(values[0]).toBe(300);
    expect(values.at(-1)).toBe(0);
  });

  it('settles without overshoot by default, and stops early once at rest', () => {
    const values = springValues(300, 0);
    expect(values.every((y) => y >= 0)).toBe(true);
    // 0.35 s response: well under the 3 s cap at 60 frames a second.
    expect(values.length).toBeLessThan(60);
  });

  it('a bouncier spring after a flick goes a little past the target', () => {
    const values = springValues(300, 0, { dampingRatio: 0.6, velocity: -2000 });
    expect(Math.min(...values)).toBeLessThan(0);
    expect(values.at(-1)).toBe(0);
  });

  it('an overdamped spring creeps in without passing the target', () => {
    const values = springValues(0, 200, { dampingRatio: 1.5 });
    expect(values.every((y) => y <= 200)).toBe(true);
    expect(values.at(-1)).toBe(200);
  });

  it('a faster response gets there sooner', () => {
    expect(springValues(300, 0, { response: 0.2 }).length).toBeLessThan(springValues(300, 0, { response: 0.6 }).length);
  });
});

describe('flicks and edges', () => {
  it('a flick carries on in its direction, further when faster', () => {
    expect(project(1000)).toBeGreaterThan(0);
    expect(project(-1000)).toBeLessThan(0);
    expect(project(2000)).toBeCloseTo(2 * project(1000));
  });

  it('past an edge the element follows less and less, keeping the direction', () => {
    expect(rubberband(100, 800)).toBeLessThan(100);
    expect(rubberband(-100, 800)).toBeCloseTo(-rubberband(100, 800));
    expect(rubberband(400, 800) - rubberband(300, 800)).toBeLessThan(rubberband(100, 800));
  });
});
