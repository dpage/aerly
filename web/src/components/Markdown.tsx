import { Fragment, type ReactNode } from 'react';
import { Box, Link, Typography } from '@mui/material';
import type { SxProps, Theme } from '@mui/material';

import { parseMarkdown, type Block, type Inline } from '../lib/markdown';

interface Props {
  /** The raw Markdown source (e.g. a trip's description). */
  text: string;
  /** Extra styling applied to the wrapper. */
  sx?: SxProps<Theme>;
}

function renderInline(nodes: Inline[]): ReactNode[] {
  return nodes.map((n, i) => {
    switch (n.type) {
      case 'text':
        return <Fragment key={i}>{n.value}</Fragment>;
      case 'strong':
        return <strong key={i}>{renderInline(n.children)}</strong>;
      case 'em':
        return <em key={i}>{renderInline(n.children)}</em>;
      case 'code':
        return (
          <Box
            key={i}
            component="code"
            sx={{
              px: 0.5,
              borderRadius: 0.5,
              fontFamily: 'monospace',
              fontSize: '0.85em',
              bgcolor: 'action.hover',
            }}
          >
            {n.value}
          </Box>
        );
      case 'link':
        return (
          <Link key={i} href={n.href} target="_blank" rel="noopener noreferrer">
            {renderInline(n.children)}
          </Link>
        );
    }
  });
}

// ATX heading levels map onto restrained typography — a trip note shouldn't
// shout with a page-sized H1. Level clamps to the table's range.
const HEADING_VARIANT = [
  'subtitle1',
  'subtitle1',
  'subtitle2',
  'subtitle2',
  'body2',
  'body2',
] as const;

function renderBlock(block: Block, key: number): ReactNode {
  switch (block.type) {
    case 'heading':
      return (
        <Typography
          key={key}
          component={`h${block.level}` as 'h1'}
          variant={HEADING_VARIANT[block.level - 1]}
          sx={{ fontWeight: 600, mt: 1.5, mb: 0.5, '&:first-of-type': { mt: 0 } }}
        >
          {renderInline(block.children)}
        </Typography>
      );
    case 'paragraph':
      return (
        <Typography key={key} variant="body2" sx={{ my: 1, '&:first-of-type': { mt: 0 } }}>
          {renderInline(block.children)}
        </Typography>
      );
    case 'list':
      return (
        <Box
          key={key}
          component={block.ordered ? 'ol' : 'ul'}
          sx={{ my: 1, pl: 3, '&:first-of-type': { mt: 0 } }}
        >
          {block.items.map((item, i) => (
            <Typography key={i} component="li" variant="body2">
              {renderInline(item)}
            </Typography>
          ))}
        </Box>
      );
  }
}

/**
 * Render trusted-looking-but-untrusted Markdown as React elements (never raw
 * HTML), so text is escaped by React and only scheme-checked links become
 * anchors. Renders nothing for blank input.
 */
export default function Markdown({ text, sx }: Props) {
  const blocks = parseMarkdown(text);
  if (blocks.length === 0) return null;
  return (
    <Box sx={{ wordBreak: 'break-word', ...sx }}>{blocks.map((b, i) => renderBlock(b, i))}</Box>
  );
}
