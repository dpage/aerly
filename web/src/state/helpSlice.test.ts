import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../api/client', () => ({ ApiError: class {}, api: {} }));

import { useStore } from './store';

beforeEach(() => {
  useStore.setState({ helpOpen: false, helpPage: null }, false);
});

describe('helpSlice', () => {
  it('defaults to closed with no page', () => {
    expect(useStore.getState().helpOpen).toBe(false);
    expect(useStore.getState().helpPage).toBeNull();
  });

  it('openHelp() with no context opens on the overview (null page)', () => {
    useStore.getState().openHelp();
    expect(useStore.getState().helpOpen).toBe(true);
    expect(useStore.getState().helpPage).toBeNull();
  });

  it('openHelp(context) seeds the topic', () => {
    useStore.getState().openHelp('sharing');
    expect(useStore.getState().helpOpen).toBe(true);
    expect(useStore.getState().helpPage).toBe('sharing');
  });

  it('closeHelp() closes the panel', () => {
    useStore.getState().openHelp('sharing');
    useStore.getState().closeHelp();
    expect(useStore.getState().helpOpen).toBe(false);
  });
});
