import { describe, expect, it } from 'vitest';

import { errorMessage, isNetworkError } from './helpers';

describe('errorMessage', () => {
  it('returns an Error message', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom');
  });
  it('stringifies a non-Error', () => {
    expect(errorMessage('plain')).toBe('plain');
    expect(errorMessage(42)).toBe('42');
  });
});

describe('isNetworkError', () => {
  it.each([
    'Failed to fetch',
    'Load failed',
    'NetworkError when attempting to fetch resource',
    'Network request failed',
    'FetchEvent.respondWith received an error: no-response :: [{"url":"https://aerly.me/api/trips"}]',
    'The Internet connection appears to be offline.',
  ])('flags connectivity failure: %s', (msg) => {
    expect(isNetworkError(msg)).toBe(true);
  });

  it.each(['That invitation is not for your account', 'Title is required', 'HTTP 500'])(
    'leaves real app errors alone: %s',
    (msg) => {
      expect(isNetworkError(msg)).toBe(false);
    },
  );
});
