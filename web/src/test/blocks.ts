import type { PlanPart, TimeBlock } from '../api/types';

/** A TimeBlock for a test fixture: a confirmed scheduled time in `tz`, or a
 * floating one when tz is blank. Override any field with `over`. */
export function block(at: string, tz = 'UTC', over: Partial<TimeBlock> = {}): TimeBlock {
  return {
    at,
    tz,
    tz_source: tz ? 'confirmed' : 'floating',
    place: '',
    kind: 'scheduled',
    delay_min: 0,
    ...over,
  };
}

/** The start/end blocks a part fixture factory should carry, derived from the
 * factory's defaults (`base`) and the test's overrides (`over`), so a test that
 * only moves `starts_at` or a zone still gets matching blocks. An explicit
 * `start`/`end` in `over` wins when spread after this. */
export function partBlocks(
  base: Partial<PlanPart>,
  over: Partial<PlanPart>,
): Pick<PlanPart, 'start' | 'end'> {
  const startSrc = over.start_tz_source ?? base.start_tz_source;
  const endSrc = over.end_tz_source ?? base.end_tz_source;
  // As the server builds them: a floating end's block has no zone, whatever
  // airport fallback its raw zone carries, and an end never borrows the
  // start's zone.
  const startTz = startSrc === 'floating' ? '' : (over.start_tz ?? base.start_tz ?? 'UTC');
  const endTz = endSrc === 'floating' ? '' : (over.end_tz ?? base.end_tz ?? 'UTC');
  const endAt = 'ends_at' in over ? over.ends_at : base.ends_at;
  return {
    start: block(
      over.effective_at ?? over.starts_at ?? base.starts_at ?? '',
      startTz,
      startSrc ? { tz_source: startSrc } : {},
    ),
    end: endAt ? block(endAt, endTz, endSrc ? { tz_source: endSrc } : {}) : undefined,
  };
}
