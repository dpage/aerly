import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { AdapterDateFns } from '@mui/x-date-pickers/AdapterDateFnsV3';

import type { Plan, PlanPart } from '../api/types';

// Unlike the other PlanEditDialog suites, this one runs the real MUI
// DateTimePicker, since what it guards is how the picker holds its value.

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      updatePlan: vi.fn(),
      updatePlanPart: vi.fn(),
      splitPlanPart: vi.fn(),
      setError: vi.fn(),
      setNotice: vi.fn(),
      capabilities: { attachments_enabled: false },
    }),
}));

vi.mock('../api/client', () => ({
  api: { resolveMapsUrl: vi.fn() },
  ApiError: class extends Error {},
}));

import PlanEditDialog from './PlanEditDialog';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 100,
    plan_id: 42,
    type: 'activity',
    seq: 0,
    starts_at: '2026-03-29T05:30:00Z',
    start_tz: 'America/New_York',
    start_label: 'Brunch',
    status: 'planned',
    effective_at: '2026-03-29T05:30:00Z',
    ...over,
  };
}

function plan(parts: PlanPart[]): Plan {
  return {
    id: 42,
    trip_id: 7,
    type: 'activity',
    title: 'Brunch',
    confirmation_ref: '',
    ticket_number: '',
    notes: '',
    source: '',
    cost_currency: '',
    passenger_ids: [],
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: '',
    visibility: { mode: 'everyone', user_ids: [] },
    alert_opted_in: false,
    parts,
    attachments: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

describe('PlanEditDialog — picker across timezones', () => {
  // A browser in London. Node re-reads TZ when it is assigned.
  const savedTz = process.env.TZ;
  beforeAll(() => {
    process.env.TZ = 'Europe/London';
  });
  afterAll(() => {
    process.env.TZ = savedTz;
  });

  it("shows a part's wall-clock that falls in the browser's spring-forward gap", () => {
    // 01:30 in New York on 29 March 2026 exists (New York moved to EDT on
    // 8 March) but is inside London's 01:00-02:00 gap that morning. A
    // browser-local Date would roll it on to 02:30; the zoned value keeps it.
    expect(new Date(2026, 2, 29, 1, 30).getHours()).toBe(2);
    // Under the app's own (date-fns) provider, as App.tsx mounts it.
    render(
      <LocalizationProvider dateAdapter={AdapterDateFns}>
        <PlanEditDialog open plan={plan([part()])} onClose={() => {}} />
      </LocalizationProvider>,
    );
    expect(screen.getAllByLabelText('Date & time')[0]).toHaveValue('03/29/2026 01:30');
  });
});
