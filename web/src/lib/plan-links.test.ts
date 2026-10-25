import { describe, it, expect } from 'vitest';

import { cleanLinks, isHttpUrl, isInlineImage, linksChanged } from './plan-links';

describe('isHttpUrl', () => {
  it('accepts absolute http(s) URLs only', () => {
    expect(isHttpUrl(' https://example.com/t.pdf ')).toBe(true);
    expect(isHttpUrl('http://example.com')).toBe(true);
    expect(isHttpUrl('javascript:alert(1)')).toBe(false);
    expect(isHttpUrl('ftp://example.com/x')).toBe(false);
    expect(isHttpUrl('example.com/x')).toBe(false);
    expect(isHttpUrl('')).toBe(false);
  });
});

describe('cleanLinks', () => {
  it('trims rows and drops wholly blank ones', () => {
    expect(
      cleanLinks([
        { title: ' Ticket ', url: ' https://example.com/t ' },
        { title: ' ', url: '' },
        { title: '', url: 'https://example.com/b' },
      ]),
    ).toEqual([
      { title: 'Ticket', url: 'https://example.com/t' },
      { title: '', url: 'https://example.com/b' },
    ]);
  });
});

describe('linksChanged', () => {
  const saved = [{ id: 1, title: 'example.com', url: 'https://example.com/t' }];

  it('is false for the same links', () => {
    expect(linksChanged(saved, [{ title: 'example.com', url: 'https://example.com/t' }])).toBe(false);
  });

  it('treats a blank title as the host the server would fill in', () => {
    expect(linksChanged(saved, [{ title: '', url: 'https://example.com/t' }])).toBe(false);
  });

  it('spots added, removed, retitled and re-pointed links', () => {
    expect(linksChanged(saved, [])).toBe(true);
    expect(linksChanged([], [{ title: 'A', url: 'https://example.com/a' }])).toBe(true);
    expect(linksChanged(saved, [{ title: 'Ticket', url: 'https://example.com/t' }])).toBe(true);
    expect(linksChanged(saved, [{ title: 'example.com', url: 'https://example.com/u' }])).toBe(true);
  });

  it('copes with a blank title on an unparseable URL', () => {
    expect(linksChanged(saved, [{ title: '', url: 'not a url' }])).toBe(true);
    expect(
      linksChanged([{ id: 1, title: '', url: 'not a url' }], [{ title: '', url: 'not a url' }]),
    ).toBe(false);
  });
});

describe('isInlineImage', () => {
  it('matches https URLs whose path ends in an image extension', () => {
    expect(isInlineImage('https://example.com/pass.PNG')).toBe(true);
    expect(isInlineImage('https://example.com/a/b.jpeg?sig=1')).toBe(true);
    expect(isInlineImage('https://example.com/x.avif')).toBe(true);
  });

  it('rejects http, other files and junk', () => {
    expect(isInlineImage('http://example.com/pass.png')).toBe(false);
    expect(isInlineImage('https://example.com/ticket.pdf')).toBe(false);
    expect(isInlineImage('https://example.com/?f=x.png')).toBe(false);
    expect(isInlineImage('nope')).toBe(false);
  });
});
