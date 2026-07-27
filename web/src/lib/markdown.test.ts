import { describe, it, expect } from 'vitest';
import { parseMarkdown, parseInline, sanitizeHref, type Block, type Inline } from './markdown';

describe('sanitizeHref', () => {
  it('accepts http(s) and mailto', () => {
    expect(sanitizeHref('https://ex.com')).toBe('https://ex.com');
    expect(sanitizeHref('http://ex.com')).toBe('http://ex.com');
    expect(sanitizeHref('MAILTO:a@b.com')).toBe('MAILTO:a@b.com');
  });

  it('accepts relative and anchor links, trims whitespace', () => {
    expect(sanitizeHref('  /trips/1 ')).toBe('/trips/1');
    expect(sanitizeHref('#section')).toBe('#section');
  });

  it('rejects blank and dangerous schemes', () => {
    expect(sanitizeHref('')).toBeNull();
    expect(sanitizeHref('   ')).toBeNull();
    expect(sanitizeHref('javascript:alert(1)')).toBeNull();
    expect(sanitizeHref('data:text/html,x')).toBeNull();
  });
});

describe('parseInline', () => {
  it('returns a single text node for plain text', () => {
    expect(parseInline('just words')).toEqual([{ type: 'text', value: 'just words' }]);
  });

  it('parses an inline code span literally', () => {
    expect(parseInline('a `b*c` d')).toEqual([
      { type: 'text', value: 'a ' },
      { type: 'code', value: 'b*c' },
      { type: 'text', value: ' d' },
    ]);
  });

  it('parses bold in preference to italic', () => {
    expect(parseInline('**x**')).toEqual([
      { type: 'strong', children: [{ type: 'text', value: 'x' }] },
    ]);
  });

  it('parses italic with either delimiter', () => {
    expect(parseInline('_y_')).toEqual([{ type: 'em', children: [{ type: 'text', value: 'y' }] }]);
    expect(parseInline('*z*')).toEqual([{ type: 'em', children: [{ type: 'text', value: 'z' }] }]);
  });

  it('nests emphasis inside bold', () => {
    expect(parseInline('**a _b_**')).toEqual([
      {
        type: 'strong',
        children: [
          { type: 'text', value: 'a ' },
          { type: 'em', children: [{ type: 'text', value: 'b' }] },
        ],
      },
    ]);
  });

  it('parses a link with sanitized href', () => {
    expect(parseInline('see [home](https://ex.com) now')).toEqual([
      { type: 'text', value: 'see ' },
      { type: 'link', href: 'https://ex.com', children: [{ type: 'text', value: 'home' }] },
      { type: 'text', value: ' now' },
    ]);
  });

  it('drops the anchor but keeps text for an unsafe link', () => {
    expect(parseInline('[click](javascript:void)')).toEqual([{ type: 'text', value: 'click' }]);
  });
});

describe('parseMarkdown', () => {
  it('returns no blocks for blank input', () => {
    expect(parseMarkdown('')).toEqual([]);
    expect(parseMarkdown('   \n\n')).toEqual([]);
  });

  it('parses a heading with its level', () => {
    expect(parseMarkdown('### Title')).toEqual<Block[]>([
      { type: 'heading', level: 3, children: [{ type: 'text', value: 'Title' }] },
    ]);
  });

  it('joins consecutive lines into one paragraph and splits on blank lines', () => {
    const blocks = parseMarkdown('one\ntwo\n\nthree');
    expect(blocks).toEqual<Block[]>([
      { type: 'paragraph', children: [{ type: 'text', value: 'one two' }] },
      { type: 'paragraph', children: [{ type: 'text', value: 'three' }] },
    ]);
  });

  it('parses an unordered list', () => {
    const blocks = parseMarkdown('- a\n- b');
    expect(blocks).toEqual<Block[]>([
      {
        type: 'list',
        ordered: false,
        items: [[{ type: 'text', value: 'a' }], [{ type: 'text', value: 'b' }]],
      },
    ]);
  });

  it('parses an ordered list', () => {
    const blocks = parseMarkdown('1. first\n2. second');
    expect(blocks).toEqual<Block[]>([
      {
        type: 'list',
        ordered: true,
        items: [[{ type: 'text', value: 'first' }], [{ type: 'text', value: 'second' }]],
      },
    ]);
  });

  it('starts a fresh list when the marker style changes', () => {
    const blocks = parseMarkdown('- a\n1. b') as Extract<Block, { type: 'list' }>[];
    expect(blocks).toHaveLength(2);
    expect(blocks[0].ordered).toBe(false);
    expect(blocks[1].ordered).toBe(true);
  });

  it('flushes an open paragraph before a heading and a list', () => {
    const blocks = parseMarkdown('intro\n# H\n- item');
    expect(blocks.map((b) => b.type)).toEqual(['paragraph', 'heading', 'list']);
  });

  it('ends a list when a plain paragraph line follows', () => {
    const blocks = parseMarkdown('- a\nafter');
    expect(blocks.map((b) => b.type)).toEqual(['list', 'paragraph']);
  });

  it('normalizes CRLF line endings', () => {
    const blocks = parseMarkdown('a\r\n\r\nb') as Array<Extract<Block, { type: 'paragraph' }>>;
    const texts = blocks.map((b) => (b.children[0] as Extract<Inline, { type: 'text' }>).value);
    expect(texts).toEqual(['a', 'b']);
  });
});
