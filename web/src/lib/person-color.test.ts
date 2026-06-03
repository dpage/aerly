import { describe, it, expect } from 'vitest';

import { personColor } from './person-color';

describe('personColor', () => {
  it('returns null for missing identities so callers fall back to the type colour', () => {
    expect(personColor(undefined)).toBeNull();
    expect(personColor(null)).toBeNull();
    expect(personColor(0)).toBeNull();
    expect(personColor('')).toBeNull();
  });

  it('is deterministic — the same key always yields the same colour', () => {
    expect(personColor(42)).toBe(personColor(42));
    expect(personColor('42')).toBe(personColor(42)); // number/string keys coincide
  });

  it('produces a valid HSL colour with the fixed saturation/lightness', () => {
    const c = personColor(7);
    expect(c).toMatch(/^hsl\(\d{1,3}, 70%, 42%\)$/);
  });

  it('keeps the hue within [0, 360)', () => {
    for (const k of [1, 2, 3, 99, 1000, 123456]) {
      const hue = Number(/^hsl\((\d+),/.exec(personColor(k)!)![1]);
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
  });

  it('spreads distinct identities across different hues', () => {
    const hues = new Set([1, 2, 3, 4, 5, 6, 7, 8].map((k) => personColor(k)));
    // Not a strict guarantee, but consecutive small ids must not all collide.
    expect(hues.size).toBeGreaterThan(5);
  });
});
