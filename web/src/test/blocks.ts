import type { TimeBlock } from '../api/types';

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
