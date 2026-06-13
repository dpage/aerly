import { describe, it, expect } from 'vitest';

import type { PlanPart, PlanType, User } from '../api/types';
import { filterTrackerParts } from './tracker-filter';

function user(id: number): User {
  return {
    id,
    username: `u${id}`,
    name: `User ${id}`,
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    home_address: '',
  };
}

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'flight',
    seq: 0,
    starts_at: '2026-01-01T00:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: '',
    start_address: '',
    end_label: '',
    end_address: '',
    status: 'planned',
    effective_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

const ALL: PlanType[] = ['flight', 'train', 'hotel', 'ground', 'dining', 'excursion'];

describe('filterTrackerParts', () => {
  it('returns everything when nothing is filtered', () => {
    const parts = ALL.map((type, i) => part({ id: i, type }));
    const out = filterTrackerParts(parts, { mineOnly: false, hiddenTypes: [], meId: 1 });
    expect(out).toHaveLength(ALL.length);
  });

  it('drops parts whose type is hidden', () => {
    const parts = ALL.map((type, i) => part({ id: i, type }));
    const out = filterTrackerParts(parts, {
      mineOnly: false,
      hiddenTypes: ['excursion', 'dining'],
      meId: 1,
    });
    expect(out.map((p) => p.type).sort()).toEqual(['flight', 'ground', 'hotel', 'train']);
  });

  it('keeps only parts I am a passenger on when mineOnly is set', () => {
    const mine = part({ id: 1, passengers: [user(7), user(9)] });
    const theirs = part({ id: 2, passengers: [user(9)] });
    const out = filterTrackerParts([mine, theirs], { mineOnly: true, hiddenTypes: [], meId: 7 });
    expect(out.map((p) => p.id)).toEqual([1]);
  });

  it('counts a part I own (no passenger list) as mine', () => {
    const mineByOwner = part({ id: 1, owner: user(7) });
    const theirs = part({ id: 2, owner: user(9), passengers: [user(9)] });
    const out = filterTrackerParts([mineByOwner, theirs], {
      mineOnly: true,
      hiddenTypes: [],
      meId: 7,
    });
    expect(out.map((p) => p.id)).toEqual([1]);
  });

  it('applies type and ownership filters together', () => {
    const myFlight = part({ id: 1, type: 'flight', passengers: [user(7)] });
    const myHotel = part({ id: 2, type: 'hotel', passengers: [user(7)] });
    const theirFlight = part({ id: 3, type: 'flight', passengers: [user(9)] });
    const out = filterTrackerParts([myFlight, myHotel, theirFlight], {
      mineOnly: true,
      hiddenTypes: ['hotel'],
      meId: 7,
    });
    expect(out.map((p) => p.id)).toEqual([1]);
  });

  it('yields nothing for mineOnly with no current user', () => {
    const parts = [part({ id: 1, passengers: [user(7)] })];
    expect(filterTrackerParts(parts, { mineOnly: true, hiddenTypes: [], meId: undefined })).toEqual(
      [],
    );
  });
});
