import type { ReactNode } from 'react';
import { Children, isValidElement } from 'react';
import { Box, Stack, Typography } from '@mui/material';

import { fmtDateTime, fmtUTC } from '../lib/format';

// Small presentational primitives shared by FlightDetailCard and PartDetailBlock
// — a labelled section with label/value rows. A Section/Row renders nothing when
// every value is empty, so a sparse part shows a correspondingly short block.

// rowRendersEmpty mirrors the null conditions inside Row/TimeRow so Section can
// decide whether it has any visible content. We must inspect props rather than
// truthiness of the child: a <Row value={null} /> is still a (truthy) element,
// so checking the element itself would never collapse the heading.
function rowRendersEmpty(child: ReactNode): boolean {
  if (child == null || child === false || child === '') return true;
  if (isValidElement(child)) {
    if (child.type === Row) {
      const v = (child.props as { value?: ReactNode }).value;
      return v == null || v === '' || v === false;
    }
    if (child.type === TimeRow) {
      return !(child.props as { iso?: string }).iso;
    }
  }
  // Unknown child (Mono, nested Section, custom node) — assume it renders.
  return false;
}

export function Section({
  title,
  titleAdornment,
  children,
}: {
  title: string;
  titleAdornment?: ReactNode;
  children: ReactNode;
}) {
  // Hide the whole section when every Row/TimeRow would render null.
  if (Children.toArray(children).every(rowRendersEmpty)) return null;
  return (
    <Box>
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 0.5 }}>
        <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1.2 }}>
          {title}
        </Typography>
        {titleAdornment}
      </Stack>
      <Stack spacing={0.25}>{children}</Stack>
    </Box>
  );
}

/** A label/value row. Returns null (so the parent Section can collapse) when the
 * value is empty/nullish. */
export function Row({ label, value }: { label: string; value: ReactNode }) {
  if (value == null || value === '' || value === false) return null;
  return (
    <Stack direction="row" spacing={1} sx={{ fontSize: 13 }}>
      <Typography variant="body2" color="text.secondary" sx={{ minWidth: 120, flex: 'none' }}>
        {label}
      </Typography>
      <Box component="span" sx={{ fontSize: 13, color: 'text.primary' }}>
        {value}
      </Box>
    </Stack>
  );
}

/** A schedule row: airport-local time with a secondary UTC line. Null when the
 * instant is missing. */
export function TimeRow({ label, iso, tz }: { label: string; iso?: string; tz?: string }) {
  if (!iso) return null;
  return (
    <Row
      label={label}
      value={
        <Stack component="span" sx={{ display: 'inline-flex' }}>
          <Box component="span">{fmtDateTime(iso, tz)}</Box>
          {tz && (
            <Typography variant="caption" color="text.secondary">
              {fmtUTC(iso)}
            </Typography>
          )}
        </Stack>
      }
    />
  );
}

/** Monospaced inline value, for codes (ICAO24, confirmation refs). */
export function Mono({ children }: { children: ReactNode }) {
  return (
    <Box component="span" sx={{ fontFamily: 'monospace' }}>
      {children}
    </Box>
  );
}
