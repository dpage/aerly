import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => ({
  setTripReminder: vi.fn(),
  clearTripReminder: vi.fn(),
  setError: vi.fn(),
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      setTripReminder: h.setTripReminder,
      clearTripReminder: h.clearTripReminder,
      setError: h.setError,
    }),
}));

import TripReminderToggle from './TripReminderToggle';
import type { Trip } from '../api/types';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 7,
    name: 'NYC',
    destination: 'New York',
    my_role: 'owner',
    viewer_is_passenger: false,
    members: [],
    passenger_ids: [],
    tags: [],
    reminder_opted_in: false,
    reminder_lead_hours: 24,
    created_at: '',
    updated_at: '',
    ...over,
  } as Trip;
}

beforeEach(() => {
  vi.clearAllMocks();
  h.setTripReminder.mockResolvedValue(undefined);
  h.clearTripReminder.mockResolvedValue(undefined);
});

describe('TripReminderToggle', () => {
  it('is off and hides the lead field when not opted in', () => {
    render(<TripReminderToggle trip={trip()} />);
    expect(screen.getByRole('checkbox', { name: /email me reminders/i })).not.toBeChecked();
    expect(screen.queryByLabelText(/reminder lead time in hours/i)).toBeNull();
  });

  it('opts in with the default lead and shows the lead field', async () => {
    render(<TripReminderToggle trip={trip()} />);
    await userEvent.click(screen.getByRole('checkbox', { name: /email me reminders/i }));
    await waitFor(() => expect(h.setTripReminder).toHaveBeenCalledWith(7, 24));
    expect(screen.getByLabelText(/reminder lead time in hours/i)).toBeInTheDocument();
  });

  it('clears the opt-in when toggled off', async () => {
    render(<TripReminderToggle trip={trip({ reminder_opted_in: true, reminder_lead_hours: 12 })} />);
    await userEvent.click(screen.getByRole('checkbox', { name: /email me reminders/i }));
    await waitFor(() => expect(h.clearTripReminder).toHaveBeenCalledWith(7));
  });

  it('saves a changed lead time on blur', async () => {
    render(<TripReminderToggle trip={trip({ reminder_opted_in: true, reminder_lead_hours: 24 })} />);
    const field = screen.getByLabelText(/reminder lead time in hours/i);
    await userEvent.clear(field);
    await userEvent.type(field, '6');
    await userEvent.tab(); // blur
    await waitFor(() => expect(h.setTripReminder).toHaveBeenCalledWith(7, 6));
  });

  it('reverts and reports an error when the opt-in fails', async () => {
    h.setTripReminder.mockRejectedValue(new Error('boom'));
    render(<TripReminderToggle trip={trip()} />);
    const toggle = screen.getByRole('checkbox', { name: /email me reminders/i });
    await userEvent.click(toggle);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('boom'));
    expect(toggle).not.toBeChecked();
  });

  it('keeps the switch on when a lead-time save fails from the blur path', async () => {
    h.setTripReminder.mockRejectedValue(new Error('save boom'));
    render(<TripReminderToggle trip={trip({ reminder_opted_in: true, reminder_lead_hours: 24 })} />);
    const toggle = screen.getByRole('checkbox', { name: /email me reminders/i });
    const field = screen.getByLabelText(/reminder lead time in hours/i);
    await userEvent.clear(field);
    await userEvent.type(field, '6');
    await userEvent.tab();
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
    // The failed save was not a disable — the switch must stay on, not flip off.
    expect(toggle).toBeChecked();
  });

  it('falls back to a 24h lead when the field is non-positive', async () => {
    render(<TripReminderToggle trip={trip({ reminder_opted_in: true, reminder_lead_hours: 24 })} />);
    const field = screen.getByLabelText(/reminder lead time in hours/i);
    await userEvent.clear(field);
    await userEvent.type(field, '0');
    await userEvent.tab();
    await waitFor(() => expect(h.setTripReminder).toHaveBeenCalledWith(7, 24));
  });
});
