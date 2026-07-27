import { describe, expect, it, beforeEach } from 'vitest';
import { THEMES, DEFAULT_CATS, expandThemeChildren, loadCats, saveCats, SUBCATEGORY_ICONS, SUBCATEGORY_LABELS } from './exploreCategories';

describe('exploreCategories', () => {
  beforeEach(() => window.localStorage.clear());

  it('every theme child has a label and an icon', () => {
    for (const theme of THEMES) {
      for (const child of theme.children) {
        expect(SUBCATEGORY_LABELS[child]).toBeTruthy();
        expect(SUBCATEGORY_ICONS[child]).toBeTruthy();
      }
    }
  });

  it('expandThemeChildren returns the theme’s children', () => {
    const food = THEMES.find((t) => t.key === 'food_drink')!;
    expect(expandThemeChildren(food)).toEqual(food.children);
  });

  it('loadCats returns defaults when nothing stored', () => {
    expect(loadCats()).toEqual(DEFAULT_CATS);
  });

  it('saveCats round-trips and drops unknown keys', () => {
    saveCats(['bars', 'nightclubs']);
    expect(loadCats()).toEqual(['bars', 'nightclubs']);
    window.localStorage.setItem('aerly.explore.subcats', JSON.stringify(['bars', 'legacy_key']));
    expect(loadCats()).toEqual(['bars']);
  });

  it('honours an explicit empty selection', () => {
    saveCats([]);
    expect(loadCats()).toEqual([]);
  });

  it('loadCats falls back to defaults on malformed JSON', () => {
    window.localStorage.setItem('aerly.explore.subcats', 'not json');
    expect(loadCats()).toEqual(DEFAULT_CATS);
  });

  it('loadCats falls back to defaults when the stored value is not an array', () => {
    window.localStorage.setItem('aerly.explore.subcats', JSON.stringify({ nope: 1 }));
    expect(loadCats()).toEqual(DEFAULT_CATS);
  });

  it('saveCats swallows storage errors', () => {
    const original = window.localStorage;
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: () => null,
        setItem: () => {
          throw new Error('quota exceeded');
        },
        removeItem: () => {},
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    try {
      expect(() => saveCats(['bars'])).not.toThrow();
    } finally {
      Object.defineProperty(window, 'localStorage', { configurable: true, value: original });
    }
  });
});
