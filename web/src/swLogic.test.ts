import { describe, expect, it, vi } from 'vitest';

import {
  DEFAULT_TITLE,
  focusOrOpen,
  hasFocusedClient,
  parsePushPayload,
  toNotification,
  urlForNotification,
  type FocusableClient,
} from './swLogic';

describe('parsePushPayload', () => {
  it('parses a full payload', () => {
    const p = parsePushPayload(
      JSON.stringify({ title: 'BA882 delayed', body: 'now 14:00', url: '/trips/7', tag: 't', kind: 'alert' }),
    );
    expect(p).toEqual({ title: 'BA882 delayed', body: 'now 14:00', url: '/trips/7', tag: 't', kind: 'alert' });
  });

  it('falls back to defaults for missing fields', () => {
    const p = parsePushPayload(JSON.stringify({ body: 'hi' }));
    expect(p.title).toBe(DEFAULT_TITLE);
    expect(p.body).toBe('hi');
    expect(p.url).toBeUndefined();
  });

  it('defaults the title when it is empty', () => {
    const p = parsePushPayload(JSON.stringify({ title: '', body: 'x' }));
    expect(p.title).toBe(DEFAULT_TITLE);
  });

  it('treats non-JSON as a body-only notification', () => {
    const p = parsePushPayload('just text');
    expect(p).toEqual({ title: DEFAULT_TITLE, body: 'just text' });
  });

  it('handles empty / null input', () => {
    expect(parsePushPayload(undefined)).toEqual({ title: DEFAULT_TITLE, body: '' });
    expect(parsePushPayload(null)).toEqual({ title: DEFAULT_TITLE, body: '' });
    expect(parsePushPayload('')).toEqual({ title: DEFAULT_TITLE, body: '' });
  });

  it('ignores wrong-typed fields', () => {
    const p = parsePushPayload(JSON.stringify({ title: 5, body: 9, url: 1, tag: {}, kind: [] }));
    expect(p.title).toBe(DEFAULT_TITLE);
    expect(p.body).toBe('');
    expect(p.url).toBeUndefined();
    expect(p.tag).toBeUndefined();
    expect(p.kind).toBeUndefined();
  });
});

describe('toNotification', () => {
  it('maps a payload to showNotification args with the url in data', () => {
    const { title, options } = toNotification({ title: 'T', body: 'B', url: '/trips/1', tag: 'g' });
    expect(title).toBe('T');
    expect(options.body).toBe('B');
    expect(options.tag).toBe('g');
    expect((options.data as { url: string }).url).toBe('/trips/1');
  });

  it('defaults the data url to root when absent', () => {
    const { options } = toNotification({ title: 'T', body: 'B' });
    expect((options.data as { url: string }).url).toBe('/');
  });
});

describe('hasFocusedClient', () => {
  const mk = (focused: boolean): FocusableClient => ({
    focused,
    url: '/',
    focus: vi.fn().mockResolvedValue(undefined),
  });

  it('is true when any client is focused', () => {
    expect(hasFocusedClient([mk(false), mk(true)])).toBe(true);
  });

  it('is false when none are focused, or the list is empty', () => {
    expect(hasFocusedClient([mk(false), mk(false)])).toBe(false);
    expect(hasFocusedClient([])).toBe(false);
  });
});

describe('urlForNotification', () => {
  it('reads a string url from data', () => {
    expect(urlForNotification({ url: '/trips/9' })).toBe('/trips/9');
  });
  it('defaults to root for missing/empty/non-object data', () => {
    expect(urlForNotification({ url: '' })).toBe('/');
    expect(urlForNotification({})).toBe('/');
    expect(urlForNotification(null)).toBe('/');
    expect(urlForNotification('nope')).toBe('/');
    expect(urlForNotification({ url: 42 })).toBe('/');
  });
});

describe('focusOrOpen', () => {
  it('navigates and focuses an existing client', async () => {
    const navigate = vi.fn().mockResolvedValue(undefined);
    const focus = vi.fn().mockResolvedValue(undefined);
    const open = vi.fn().mockResolvedValue(undefined);
    await focusOrOpen('/trips/2', [{ focused: false, url: '/', focus, navigate }], open);
    expect(navigate).toHaveBeenCalledWith('/trips/2');
    expect(focus).toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
  });

  it('focuses an existing client that cannot navigate', async () => {
    const focus = vi.fn().mockResolvedValue(undefined);
    const open = vi.fn().mockResolvedValue(undefined);
    await focusOrOpen('/trips/2', [{ focused: false, url: '/', focus }], open);
    expect(focus).toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
  });

  it('opens a new window when none exists', async () => {
    const open = vi.fn().mockResolvedValue(undefined);
    await focusOrOpen('/trips/2', [], open);
    expect(open).toHaveBeenCalledWith('/trips/2');
  });
});
