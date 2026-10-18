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
  const startTz = over.start_tz ?? base.start_tz ?? 'UTC';
  const endAt = 'ends_at' in over ? over.ends_at : base.ends_at;
  const endTz = over.end_tz || over.start_tz || base.end_tz || base.start_tz || 'UTC';
  return {
    start: block(over.effective_at ?? over.starts_at ?? base.starts_at ?? '', startTz),
    end: endAt ? block(endAt, endTz) : undefined,
  };
}
