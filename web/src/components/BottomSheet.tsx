/* eslint-disable react-refresh/only-export-components --
   sheetHeightPx is a pure utility exported for use by the parent layout and
   by tests; it belongs here alongside the snap-type definitions. */
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Box, Paper } from '@mui/material';

export type SheetSnap = 'peek' | 'half' | 'full';

/** Height of the collapsed sheet: the drag handle plus a two-line summary. */
export const PEEK_PX = 64;

const SNAP_ORDER: SheetSnap[] = ['peek', 'half', 'full'];
const HALF_FRACTION = 0.45;
const FULL_FRACTION = 0.9;
/** Pointer travel below this is a tap, not a drag. */
const TAP_SLOP_PX = 8;

/** The sheet's height at a snap point within a container `containerH` px tall. */
export function sheetHeightPx(snap: SheetSnap, containerH: number): number {
  if (snap === 'half') return Math.round(containerH * HALF_FRACTION);
  if (snap === 'full') return Math.round(containerH * FULL_FRACTION);
  return PEEK_PX;
}

function nearestSnap(px: number, containerH: number): SheetSnap {
  let best: SheetSnap = 'peek';
  let bestDist = Infinity;
  for (const s of SNAP_ORDER) {
    const d = Math.abs(sheetHeightPx(s, containerH) - px);
    if (d < bestDist) {
      best = s;
      bestDist = d;
    }
  }
  return best;
}

interface Props {
  snap: SheetSnap;
  onSnapChange: (snap: SheetSnap) => void;
  /** The always-visible grab area under the handle (a one-line plan summary).
   * Dragging it moves the sheet; tapping it raises peek to half. */
  header: ReactNode;
  /** The sheet body (the plan list); scrolls internally at half/full only. */
  children: ReactNode;
  /** Rendered riding directly above the sheet's top edge (the time scrubber),
   * moving with it and faded out at full, where no map remains to scrub. */
  above?: ReactNode;
}

/** A bottom sheet overlaying a full-bleed map (mobile layouts only): three
 * resting heights — peek / half / nearly-full — dragged by its handle, with
 * snap-to-nearest on release, tap-to-raise from peek, and ArrowUp/ArrowDown on
 * the focused handle. Must be rendered inside a `position: relative` container,
 * whose height anchors the half/full snap points. */
export default function BottomSheet({ snap, onSnapChange, header, children, above }: Props) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [containerH, setContainerH] = useState(0);
  // Live height while a drag is in flight; null = parked at `snap`.
  const [dragPx, setDragPx] = useState<number | null>(null);
  const dragRef = useRef<{ pointerId: number; startY: number; startPx: number } | null>(null);

  // Track the container height across orientation/viewport changes so the
  // snap points stay proportional.
  useEffect(() => {
    const measure = () => setContainerH(rootRef.current?.parentElement?.clientHeight ?? 0);
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, []);

  const heightPx = dragPx ?? sheetHeightPx(snap, containerH);
  // The safe-area inset keeps the peek row clear of the iPhone home bar.
  const height = `calc(${heightPx}px + env(safe-area-inset-bottom))`;
  const transition =
    dragPx != null ? 'none' : 'height 0.25s ease, bottom 0.25s ease, opacity 0.25s ease';
  const aboveHidden = snap === 'full' && dragPx == null;

  const onPointerDown = (e: React.PointerEvent<HTMLElement>) => {
    dragRef.current = { pointerId: e.pointerId, startY: e.clientY, startPx: heightPx };
    e.currentTarget.setPointerCapture?.(e.pointerId);
  };
  const onPointerMove = (e: React.PointerEvent<HTMLElement>) => {
    const d = dragRef.current;
    if (!d || e.pointerId !== d.pointerId) return;
    const px = d.startPx + (d.startY - e.clientY);
    setDragPx(Math.max(PEEK_PX, Math.min(px, sheetHeightPx('full', containerH))));
  };
  const onPointerUp = (e: React.PointerEvent<HTMLElement>) => {
    const d = dragRef.current;
    if (!d || e.pointerId !== d.pointerId) return;
    dragRef.current = null;
    setDragPx(null);
    const travelled = d.startY - e.clientY;
    if (Math.abs(travelled) < TAP_SLOP_PX) {
      if (snap === 'peek') onSnapChange('half');
      return;
    }
    const target = nearestSnap(d.startPx + travelled, containerH);
    if (target !== snap) onSnapChange(target);
  };
  const onKeyDown = (e: React.KeyboardEvent) => {
    const i = SNAP_ORDER.indexOf(snap);
    if (e.key === 'ArrowUp' && i < SNAP_ORDER.length - 1) {
      e.preventDefault();
      onSnapChange(SNAP_ORDER[i + 1]);
    } else if (e.key === 'ArrowDown' && i > 0) {
      e.preventDefault();
      onSnapChange(SNAP_ORDER[i - 1]);
    }
  };

  return (
    <>
      {above != null && (
        <Box
          data-testid="sheet-above"
          data-hidden={aboveHidden ? '1' : '0'}
          sx={{
            // A zero-height anchor line at the sheet's top edge: the scrubber
            // inside is absolutely positioned with bottom: 0, so it grows
            // upward from this line and rides the sheet as it moves.
            position: 'absolute',
            left: 0,
            right: 0,
            height: 0,
            bottom: height,
            zIndex: 2,
            transition,
            opacity: aboveHidden ? 0 : 1,
            pointerEvents: aboveHidden ? 'none' : 'auto',
          }}
        >
          {above}
        </Box>
      )}
      <Paper
        ref={rootRef}
        elevation={8}
        square
        data-testid="bottom-sheet"
        data-snap={snap}
        sx={{
          position: 'absolute',
          left: 0,
          right: 0,
          bottom: 0,
          zIndex: 3,
          height,
          transition,
          borderTopLeftRadius: 12,
          borderTopRightRadius: 12,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
          pb: 'env(safe-area-inset-bottom)',
        }}
      >
        <Box
          data-testid="sheet-handle"
          role="button"
          tabIndex={0}
          aria-label="Plan list. Drag, or use the arrow keys, to resize"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onKeyDown={onKeyDown}
          sx={{ flex: 'none', cursor: 'grab', touchAction: 'none', pt: 0.75 }}
        >
          <Box
            sx={{ width: 36, height: 4, borderRadius: 2, bgcolor: 'divider', mx: 'auto', mb: 0.75 }}
          />
          {header}
        </Box>
        <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: snap === 'peek' ? 'hidden' : 'auto' }}>
          {children}
        </Box>
      </Paper>
    </>
  );
}
