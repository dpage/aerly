import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Plan, PlanPart } from '../api/types';
import { setMatchMedia } from '../test/setup';
import { buildPlanShareText, canShareNatively, sharePlan } from './share';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T09:00:00Z',
    ends_at: '2026-10-12T11:30:00Z',
    start_tz: 'Europe/London',
    end_tz: 'Europe/Lisbon',
    start_label: 'LHR',
    end_label: 'LIS',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
    ...over,
  };
}

function plan(over: Partial<Plan> = {}): Plan {
  return {
    id: 1,
    trip_id: 1,
    type: 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

describe('buildPlanShareText', () => {
  it('leads with the plan title', () => {
    const text = buildPlanShareText(plan({ title: 'Flight to Lisbon' }), part());
    expect(text.split('\n')[0]).toBe('Flight to Lisbon');
  });

  it('falls back to the plan-type label when there is no title', () => {
    const text = buildPlanShareText(plan({ title: '' }), part({ type: 'flight' }));
    expect(text.split('\n')[0]).toBe('Flight');
  });

  it('includes places, flight ident and booking details', () => {
    const text = buildPlanShareText(
      plan({
        title: 'Outbound',
        confirmation_ref: 'ABC123',
        ticket_number: 'TK-9',
        supplier_name: 'TAP',
        cost_amount: 120,
        cost_currency: 'GBP',
      }),
      part({ flight: { ident: 'TP1234' } as PlanPart['flight'] }),
    );
    expect(text).toContain('LHR → LIS');
    expect(text).toContain('Flight: TP1234');
    expect(text).toContain('Ticket: TK-9');
    expect(text).toContain('Ref: ABC123');
    expect(text).toContain('Supplier: TAP');
    expect(text).toContain('Cost:');
  });

  it('appends notes as a trailing block', () => {
    const text = buildPlanShareText(plan({ notes: '  Window seat  ' }), part());
    expect(text).toMatch(/\n\nWindow seat$/);
  });

  it('omits places when they only repeat the title', () => {
    const text = buildPlanShareText(
      plan({ title: 'The Grand', type: 'hotel' }),
      part({ type: 'hotel', start_label: 'The Grand', end_label: undefined }),
    );
    expect(text).not.toMatch(/The Grand\nThe Grand/);
  });
});

describe('canShareNatively / sharePlan', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setMatchMedia(false);
  });

  it('is false in a plain browser tab', () => {
    setMatchMedia(false);
    expect(canShareNatively()).toBe(false);
  });

  it('is true when installed as a PWA with a share API', () => {
    setMatchMedia(true);
    vi.stubGlobal('navigator', { ...navigator, share: vi.fn() });
    expect(canShareNatively()).toBe(true);
  });

  it('copies to the clipboard in a browser tab', async () => {
    setMatchMedia(false);
    const writeText = vi.fn(async () => {});
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
    const outcome = await sharePlan('hello', 'Title');
    expect(outcome).toBe('copied');
    expect(writeText).toHaveBeenCalledWith('hello');
  });

  it('opens the native share sheet when installed', async () => {
    setMatchMedia(true);
    const share = vi.fn(async () => {});
    vi.stubGlobal('navigator', { ...navigator, share });
    const outcome = await sharePlan('hello', 'Title');
    expect(outcome).toBe('shared');
    expect(share).toHaveBeenCalledWith({ title: 'Title', text: 'hello' });
  });

  it('reports cancellation when the user dismisses the share sheet', async () => {
    setMatchMedia(true);
    const share = vi.fn(async () => {
      throw new DOMException('cancelled', 'AbortError');
    });
    vi.stubGlobal('navigator', { ...navigator, share });
    expect(await sharePlan('hello', 'Title')).toBe('cancelled');
  });

  it('rethrows a genuine share failure', async () => {
    setMatchMedia(true);
    const share = vi.fn(async () => {
      throw new DOMException('boom', 'NotAllowedError');
    });
    vi.stubGlobal('navigator', { ...navigator, share });
    await expect(sharePlan('hello', 'Title')).rejects.toThrow('boom');
  });
});
