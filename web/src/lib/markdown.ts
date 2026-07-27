/**
 * A deliberately small Markdown parser for user-supplied trip descriptions
 * (issue #112). It produces a plain data tree — never HTML — so the renderer can
 * emit React elements and rely on React's text escaping for XSS safety. Link
 * URLs are the one attacker-controlled attribute, so they are scheme-checked
 * here (see sanitizeHref).
 *
 * Supported: ATX headings (# … ######), unordered (-,*,+) and ordered (1.) lists,
 * bold, italic, inline code, and links [text](url). Anything else renders as
 * plain text. This is intentionally not CommonMark-complete.
 */

export type Inline =
  | { type: 'text'; value: string }
  | { type: 'strong'; children: Inline[] }
  | { type: 'em'; children: Inline[] }
  | { type: 'code'; value: string }
  | { type: 'link'; href: string; children: Inline[] };

export type Block =
  | { type: 'heading'; level: number; children: Inline[] }
  | { type: 'paragraph'; children: Inline[] }
  | { type: 'list'; ordered: boolean; items: Inline[][] };

const HEADING = /^(#{1,6})\s+(.*)$/;
const UL_ITEM = /^\s*[-*+]\s+(.*)$/;
const OL_ITEM = /^\s*\d+\.\s+(.*)$/;

/**
 * Allow only links that can't execute script. http(s) and mailto cover the
 * "photo album / blog / vlog" use cases from the issue; page-relative and anchor
 * links are harmless. Everything else (javascript:, data:, vbscript:, …) is
 * rejected so the renderer drops the anchor and keeps just the text.
 */
export function sanitizeHref(raw: string): string | null {
  const href = raw.trim();
  if (href === '') return null;
  if (href.startsWith('/') || href.startsWith('#')) return href;
  if (/^(https?:|mailto:)/i.test(href)) return href;
  return null;
}

interface Match {
  index: number;
  length: number;
  node: Inline;
}

// Inline matchers, in priority order for ties on start position: code and links
// are literal-ish and win over emphasis; bold (**/__) wins over italic (*/_) so
// "**x**" isn't mis-read as italic.
function nextMatch(s: string): Match | null {
  const matchers: Array<() => Match | null> = [
    () => {
      const m = /`([^`\n]+)`/.exec(s);
      return m
        ? { index: m.index, length: m[0].length, node: { type: 'code', value: m[1] } }
        : null;
    },
    () => {
      const m = /\[([^\]\n]*)\]\(([^)\n]*)\)/.exec(s);
      if (!m) return null;
      const href = sanitizeHref(m[2]);
      const children = parseInline(m[1]);
      // A rejected scheme drops the anchor but keeps the visible text.
      const node: Inline = href ? { type: 'link', href, children } : { type: 'text', value: m[1] };
      return { index: m.index, length: m[0].length, node };
    },
    () => {
      const m = /(\*\*|__)([\s\S]+?)\1/.exec(s);
      return m
        ? {
            index: m.index,
            length: m[0].length,
            node: { type: 'strong', children: parseInline(m[2]) },
          }
        : null;
    },
    () => {
      const m = /(\*|_)([\s\S]+?)\1/.exec(s);
      return m
        ? { index: m.index, length: m[0].length, node: { type: 'em', children: parseInline(m[2]) } }
        : null;
    },
  ];
  let best: Match | null = null;
  for (const run of matchers) {
    const m = run();
    if (m && (best === null || m.index < best.index)) best = m;
    if (best && best.index === 0) break; // nothing can start earlier than 0
  }
  return best;
}

/** Parse a single line/span of text into inline nodes. */
export function parseInline(text: string): Inline[] {
  const out: Inline[] = [];
  let rest = text;
  while (rest !== '') {
    const m = nextMatch(rest);
    if (!m) {
      out.push({ type: 'text', value: rest });
      break;
    }
    if (m.index > 0) out.push({ type: 'text', value: rest.slice(0, m.index) });
    out.push(m.node);
    rest = rest.slice(m.index + m.length);
  }
  return out;
}

/** Parse a whole Markdown document into block nodes. */
export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, '\n').split('\n');
  const blocks: Block[] = [];

  let para: string[] = [];
  let list: { ordered: boolean; items: string[] } | null = null;

  const flushPara = () => {
    if (para.length > 0) {
      blocks.push({ type: 'paragraph', children: parseInline(para.join(' ')) });
      para = [];
    }
  };
  const flushList = () => {
    if (list) {
      blocks.push({
        type: 'list',
        ordered: list.ordered,
        items: list.items.map((it) => parseInline(it)),
      });
      list = null;
    }
  };

  for (const line of lines) {
    if (line.trim() === '') {
      flushPara();
      flushList();
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      flushPara();
      flushList();
      blocks.push({ type: 'heading', level: heading[1].length, children: parseInline(heading[2]) });
      continue;
    }
    const ul = UL_ITEM.exec(line);
    const ol = ul ? null : OL_ITEM.exec(line);
    if (ul || ol) {
      const ordered = ol !== null;
      const text = (ul ? ul[1] : ol![1]).trim();
      flushPara();
      if (list && list.ordered !== ordered) flushList();
      if (!list) list = { ordered, items: [] };
      list.items.push(text);
      continue;
    }
    // Plain text line: part of a paragraph (ends any open list).
    flushList();
    para.push(line.trim());
  }
  flushPara();
  flushList();
  return blocks;
}
