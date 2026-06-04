import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => ({
  setPlanReminder: vi.fn(),
  clearPlanReminder: vi.fn(),
  setError: vi.fn(),
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      setPlanReminder: h.setPlanReminder,
      clearPlanReminder: h.clearPlanReminder,
      setError: h.setError,
    }),
}));

import PlanReminderOverride from './PlanReminderOverride';
import type { Plan } from '../api/types';

function plan(over: Partial<Plan> = {}): Plan {
  return {
    id: 5,
    trip_id: 1,
    type: 'flight',
    title: 'BA1',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    alert_opted_in: false,
    reminder_override: 'inherit',
    reminder_lead_hours: 24,
    parts: [],
    created_at: '',
    updated_at: '',
    ...over,
  } as Plan;
}

// chooseMode opens the MUI select and clicks the option with the exact name
// (exact avoids "Remind me" also matching "Don't remind me").
async function chooseMode(name: string) {
  await userEvent.click(screen.getByRole('combobox', { name: /reminder/i }));
  await userEvent.click(await screen.findByRole('option', { name, exact: true }));
}

beforeEach(() => {
  vi.clearAllMocks();
  h.setPlanReminder.mockResolvedValue(undefined);
  h.clearPlanReminder.mockResolvedValue(undefined);
});

describe('PlanReminderOverride', () => {
  it('defaults to "Use trip setting" and hides the lead field', () => {
    render(<PlanReminderOverride plan={plan()} />);
    expect(screen.getByRole('combobox', { name: /reminder/i })).toHaveTextContent(/use trip setting/i);
    expect(screen.queryByLabelText(/reminder lead time in hours/i)).toBeNull();
  });

  it('forces off via the override', async () => {
    render(<PlanReminderOverride plan={plan()} />);
    await chooseMode("Don't remind me");
    await waitFor(() => expect(h.setPlanReminder).toHaveBeenCalledWith(5, false, 24));
  });

  it('forces on and shows the lead field', async () => {
    render(<PlanReminderOverride plan={plan()} />);
    await chooseMode('Remind me');
    await waitFor(() => expect(h.setPlanReminder).toHaveBeenCalledWith(5, true, 24));
    expect(screen.getByLabelText(/reminder lead time in hours/i)).toBeInTheDocument();
  });

  it('clears the override when switching back to "Use trip setting"', async () => {
    render(<PlanReminderOverride plan={plan({ reminder_override: 'on', reminder_lead_hours: 6 })} />);
    await chooseMode('Use trip setting');
    await waitFor(() => expect(h.clearPlanReminder).toHaveBeenCalledWith(5));
  });

  it('saves a changed lead on blur when on', async () => {
    render(<PlanReminderOverride plan={plan({ reminder_override: 'on', reminder_lead_hours: 24 })} />);
    const field = screen.getByLabelText(/reminder lead time in hours/i);
    await userEvent.clear(field);
    await userEvent.type(field, '3');
    await userEvent.tab();
    await waitFor(() => expect(h.setPlanReminder).toHaveBeenCalledWith(5, true, 3));
  });

  it('stringifies a non-Error rejection', async () => {
    h.setPlanReminder.mockRejectedValue('nope');
    render(<PlanReminderOverride plan={plan()} />);
    await chooseMode('Remind me');
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('nope'));
  });

  it('reports an Error rejection message', async () => {
    h.clearPlanReminder.mockRejectedValue(new Error('boom'));
    render(<PlanReminderOverride plan={plan({ reminder_override: 'off' })} />);
    await chooseMode('Use trip setting');
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('boom'));
  });

  it('falls back to a 24h lead when the field is non-positive', async () => {
    render(<PlanReminderOverride plan={plan({ reminder_override: 'on', reminder_lead_hours: 24 })} />);
    const field = screen.getByLabelText(/reminder lead time in hours/i);
    await userEvent.clear(field);
    await userEvent.type(field, '0');
    await userEvent.tab();
    await waitFor(() => expect(h.setPlanReminder).toHaveBeenCalledWith(5, true, 24));
  });
});
