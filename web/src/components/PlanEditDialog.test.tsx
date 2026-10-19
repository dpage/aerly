import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Plan } from '../api/types';

const h = vi.hoisted(() => ({
  updatePlan: vi.fn(),
  updatePlanPart: vi.fn(),
  splitPlanPart: vi.fn(),
  setError: vi.fn(),
  setNotice: vi.fn(),
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      updatePlan: h.updatePlan,
      updatePlanPart: h.updatePlanPart,
      splitPlanPart: h.splitPlanPart,
      setError: h.setError,
      setNotice: h.setNotice,
      // PlanAttachments (mounted by the dialog) reads capabilities; off here so
      // it renders nothing and these tests stay focused on the editor.
      capabilities: { attachments_enabled: false },
    }),
}));

const ha = vi.hoisted(() => ({ resolveMapsUrl: vi.fn() }));
vi.mock('../api/client', () => ({
  api: { resolveMapsUrl: ha.resolveMapsUrl },
  ApiError: class extends Error {},
}));

import type { PlanPart } from '../api/types';
import PlanEditDialog from './PlanEditDialog';
import { block } from '../test/blocks';

// The edit dialog's date/time picker, as a plain input (see the mock's notes).
vi.mock('@mui/x-date-pickers/DateTimePicker', () => import('../test/date-time-picker-mock'));

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 100,
    plan_id: 42,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T11:35:00Z',
    ends_at: '2026-10-12T19:55:00Z',
    start_tz: 'Europe/London',
    end_tz: 'America/New_York',
    start_label: 'LHR',
    end_label: 'IAD',
    status: 'planned',
    effective_at: '2026-10-12T11:35:00Z',
    ...over,
  };
}

function plan(over: Partial<Plan> = {}): Plan {
  return {
    id: 42,
    trip_id: 7,
    type: 'flight',
    title: 'BA123',
    confirmation_ref: 'XYZ',
    ticket_number: '',
    notes: 'window seat',
    source: '',
    cost_currency: '',
    passenger_ids: [],
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: '',
    visibility: { mode: 'everyone', user_ids: [] },
    alert_opted_in: false,
    parts: [],
    attachments: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

function render_(p: Plan = plan()) {
  return render(<PlanEditDialog open plan={p} onClose={() => {}} />);
}

describe('PlanEditDialog', () => {
  it('opens read-only with Save disabled while offline', () => {
    const original = navigator.onLine;
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
    try {
      render_();
      // Still opens so the full details (beyond the tile) are visible…
      expect(screen.getByRole('textbox', { name: /title/i })).toHaveValue('BA123');
      // …but it's read-only: a notice, disabled fields, no Save, Cancel→Close.
      expect(screen.getByText(/viewing only/i)).toBeInTheDocument();
      expect(screen.getByRole('textbox', { name: /title/i })).toBeDisabled();
      expect(screen.getByRole('button', { name: /save/i })).toBeDisabled();
      expect(screen.getByRole('button', { name: /close/i })).toBeInTheDocument();
    } finally {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: original });
    }
  });

  it('prefills the fields from the plan', () => {
    render_();
    expect(screen.getByRole('textbox', { name: /title/i })).toHaveValue('BA123');
    expect(screen.getByRole('textbox', { name: /confirmation/i })).toHaveValue('XYZ');
    expect(screen.getByRole('textbox', { name: /notes/i })).toHaveValue('window seat');
  });

  it('prefills ticket number and an existing cost/currency', () => {
    render_(plan({ ticket_number: 'E9', cost_amount: 100, cost_currency: 'EUR' }));
    expect(screen.getByRole('textbox', { name: /ticket number/i })).toHaveValue('E9');
    expect(screen.getByRole('spinbutton', { name: /cost/i })).toHaveValue(100);
    expect(screen.getByRole('textbox', { name: /currency/i })).toHaveValue('EUR');
  });

  it('does not call updatePlan when nothing changed', async () => {
    const onClose = vi.fn();
    render(
      <PlanEditDialog
        open
        plan={plan({ ticket_number: 'E9', cost_amount: 100, cost_currency: 'EUR' })}
        onClose={onClose}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(h.updatePlan).not.toHaveBeenCalled();
  });

  it('saves edited title/confirmation/notes', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_();
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA999',
        confirmation_ref: 'XYZ',
        ticket_number: '',
        notes: 'window seat',
        cost_currency: '',
        supplier_name: '',
        contact_email: '',
        contact_phone: '',
        website: '',
      }),
    );
  });

  it('numbers multiple parts and skips dismissed ones', () => {
    render_(
      plan({
        parts: [
          part({ id: 100 }),
          part({ id: 101 }),
          part({ id: 102, dismissed_at: '2026-01-01T00:00:00Z' }),
        ],
      }),
    );
    // Two editable parts → "Flight 1" / "Flight 2" dividers; dismissed one hidden.
    expect(screen.getByText(/Flight 1/)).toBeInTheDocument();
    expect(screen.getByText(/Flight 2/)).toBeInTheDocument();
    expect(screen.queryByText(/Flight 3/)).not.toBeInTheDocument();
  });

  it('offers "Split out" on each leg of a multi-part flight and splits it', async () => {
    h.splitPlanPart.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(
      <PlanEditDialog
        open
        plan={plan({ parts: [part({ id: 100 }), part({ id: 101 })] })}
        onClose={onClose}
      />,
    );
    const buttons = screen.getAllByRole('button', { name: /split out/i });
    expect(buttons).toHaveLength(2);
    await userEvent.click(buttons[1]);
    await waitFor(() => expect(h.splitPlanPart).toHaveBeenCalledWith(101));
    expect(onClose).toHaveBeenCalled();
  });

  it('does not offer "Split out" on a single-part plan', () => {
    render_(plan({ parts: [part({ id: 100 })] }));
    expect(screen.queryByRole('button', { name: /split out/i })).not.toBeInTheDocument();
  });

  it('offers "Split out" on each leg of a multi-part ground (transfer) plan', () => {
    render_(
      plan({
        type: 'ground',
        parts: [part({ id: 100, type: 'ground' }), part({ id: 101, type: 'ground' })],
      }),
    );
    expect(screen.getAllByRole('button', { name: /split out/i })).toHaveLength(2);
  });

  it('does not offer "Split out" on a multi-part non-linkable plan (hotel)', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({ id: 100, type: 'hotel', end_label: '' }),
          part({ id: 101, type: 'hotel', end_label: '' }),
        ],
      }),
    );
    expect(screen.queryByRole('button', { name: /split out/i })).not.toBeInTheDocument();
  });

  it('edits confirmation ref and notes and saves them', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_();
    const conf = screen.getByRole('textbox', { name: /confirmation/i });
    const notes = screen.getByRole('textbox', { name: /notes/i });
    await userEvent.clear(conf);
    await userEvent.type(conf, 'ABC');
    await userEvent.clear(notes);
    await userEvent.type(notes, 'aisle seat');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA123',
        confirmation_ref: 'ABC',
        ticket_number: '',
        notes: 'aisle seat',
        cost_currency: '',
        supplier_name: '',
        contact_email: '',
        contact_phone: '',
        website: '',
      }),
    );
  });

  it('prefills and saves edited supplier contact details', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_(
      plan({
        supplier_name: 'British Airways',
        contact_email: 'old@ba.example',
        contact_phone: '+44 1',
        website: 'ba.example',
      }),
    );
    // Prefilled from the plan.
    expect(screen.getByRole('textbox', { name: /^supplier/i })).toHaveValue('British Airways');
    expect(screen.getByRole('textbox', { name: /contact email/i })).toHaveValue('old@ba.example');
    expect(screen.getByRole('textbox', { name: /contact phone/i })).toHaveValue('+44 1');
    expect(screen.getByRole('textbox', { name: /^website/i })).toHaveValue('ba.example');

    const email = screen.getByRole('textbox', { name: /contact email/i });
    await userEvent.clear(email);
    await userEvent.type(email, 'new@ba.example');
    const site = screen.getByRole('textbox', { name: /^website/i });
    await userEvent.clear(site);
    await userEvent.type(site, 'https://ba.example/manage');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA123',
        confirmation_ref: 'XYZ',
        ticket_number: '',
        notes: 'window seat',
        cost_currency: '',
        supplier_name: 'British Airways',
        contact_email: 'new@ba.example',
        contact_phone: '+44 1',
        website: 'https://ba.example/manage',
      }),
    );
  });

  it('saves an edited ticket number and cost with currency', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_(plan({ cost_currency: 'GBP' }));
    await userEvent.type(screen.getByRole('textbox', { name: /ticket number/i }), 'E1234567890');
    await userEvent.type(screen.getByRole('spinbutton', { name: /cost/i }), '250.5');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA123',
        confirmation_ref: 'XYZ',
        ticket_number: 'E1234567890',
        notes: 'window seat',
        cost_currency: 'GBP',
        supplier_name: '',
        contact_email: '',
        contact_phone: '',
        website: '',
        cost_amount: 250.5,
      }),
    );
  });

  it('closes via Cancel without writing', async () => {
    const onClose = vi.fn();
    render(<PlanEditDialog open plan={plan()} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: /^cancel$/i }));
    expect(onClose).toHaveBeenCalled();
    expect(h.updatePlan).not.toHaveBeenCalled();
  });

  it('renders nothing on open=false', () => {
    render(<PlanEditDialog open={false} plan={plan()} onClose={() => {}} />);
    expect(screen.queryByRole('textbox', { name: /title/i })).not.toBeInTheDocument();
  });

  it('surfaces non-Error save failures by stringifying them', async () => {
    h.updatePlan.mockRejectedValue('plain string boom');
    render_();
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('plain string boom'));
  });

  it('surfaces save errors via setError', async () => {
    h.updatePlan.mockRejectedValue(new Error('save boom'));
    render_();
    // Make a change so Save actually writes (an unchanged Save is a no-op).
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
  });

  describe('time zone sources in the patch', () => {
    async function save() {
      await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
      await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
      return h.updatePlanPart.mock.calls[0][1];
    }

    it('sends confirmed when the user changes the zone', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part({ start_tz_source: 'inferred' })] }));
      const zone = screen.getAllByRole('combobox', { name: /timezone/i })[0];
      await userEvent.clear(zone);
      await userEvent.type(zone, 'Europe/Paris');
      const patch = await save();
      expect(patch.start_tz).toBe('Europe/Paris');
      expect(patch.start_tz_source).toBe('confirmed');
    });

    it('sends floating with a blank zone when the time changes', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part({ start_tz: '', start_tz_source: 'unknown' })] }));
      fireEvent.change(screen.getAllByLabelText('Date & time')[0], {
        target: { value: '2026-10-12T12:00' },
      });
      const patch = await save();
      expect(patch.start_local).toBe('2026-10-12T12:00');
      expect(patch.starts_at).toBeUndefined();
      expect(patch.start_tz).toBe('');
      expect(patch.start_tz_source).toBe('floating');
    });

    it('sends floating for a cleared end zone and never a source for a blank one besides that', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part({ end_tz: 'America/New_York', end_tz_source: 'confirmed' })] }));
      await userEvent.clear(screen.getAllByRole('combobox', { name: /timezone/i })[1]);
      const patch = await save();
      expect(patch.end_tz).toBe('');
      expect(patch.end_tz_source).toBe('floating');
    });

    it('keeps an inferred source when only the time changes', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(
        plan({
          parts: [
            part({
              start_tz_source: 'inferred',
              end_tz_source: 'inferred',
            }),
          ],
        }),
      );
      fireEvent.change(screen.getAllByLabelText('Date & time')[0], {
        target: { value: '2026-10-12T12:00' },
      });
      fireEvent.change(screen.getAllByLabelText('Date & time')[1], {
        target: { value: '2026-10-12T20:00' },
      });
      const patch = await save();
      expect(patch.start_tz).toBe('Europe/London');
      expect(patch.start_tz_source).toBe('inferred');
      expect(patch.end_tz).toBe('America/New_York');
      expect(patch.end_tz_source).toBe('inferred');
    });

    it('sends the zone and a source with the digits even when the part has no source', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part()] }));
      fireEvent.change(screen.getAllByLabelText('Date & time')[0], {
        target: { value: '2026-10-12T12:00' },
      });
      const patch = await save();
      expect(patch.start_local).toBe('2026-10-12T12:00');
      expect(patch.start_tz).toBe('Europe/London');
      expect(patch.start_tz_source).toBe('inferred');
    });

    it('never sends start_local or end_local without their zone and source', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part({ start_tz_source: 'confirmed', end_tz_source: 'confirmed' })] }));
      const whens = screen.getAllByLabelText('Date & time');
      fireEvent.change(whens[0], { target: { value: '2026-10-12T12:00' } });
      fireEvent.change(whens[1], { target: { value: '2026-10-12T21:00' } });
      const patch = await save();
      expect(patch.start_local).toBe('2026-10-12T12:00');
      expect(patch.start_tz).toBe('Europe/London');
      expect(patch.start_tz_source).toBe('confirmed');
      expect(patch.end_local).toBe('2026-10-12T21:00');
      expect(patch.end_tz).toBe('America/New_York');
      expect(patch.end_tz_source).toBe('confirmed');
    });

    it('sends inferred for an end whose zone is only borrowed from the start', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part({ end_tz: '', end_tz_source: 'unknown' })] }));
      fireEvent.change(screen.getAllByLabelText('Date & time')[1], {
        target: { value: '2026-10-12T20:00' },
      });
      const patch = await save();
      expect(patch.end_tz).toBe('Europe/London');
      expect(patch.end_tz_source).toBe('inferred');
    });

    // As the server sends a floating flight: the raw zones carry the airport
    // fallback whilst the blocks, like the stored digits, have no zone.
    function floatingFlight() {
      return part({
        start_tz_source: 'floating',
        end_tz_source: 'floating',
        start: block('2026-10-12T11:35:00Z', ''),
        end: block('2026-10-12T19:55:00Z', ''),
      });
    }

    it('shows a floating flight its stored digits with no zone', () => {
      render_(plan({ parts: [floatingFlight()] }));
      const whens = screen.getAllByLabelText('Date & time');
      expect(whens[0]).toHaveValue('2026-10-12T11:35');
      expect(whens[1]).toHaveValue('2026-10-12T19:55');
      const zones = screen.getAllByRole('combobox', { name: /timezone/i });
      expect(zones[0]).toHaveValue('');
      expect(zones[1]).toHaveValue('');
    });

    it('keeps a floating flight floating when only its time changes', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [floatingFlight()] }));
      fireEvent.change(screen.getAllByLabelText('Date & time')[0], {
        target: { value: '2026-10-12T12:00' },
      });
      const patch = await save();
      expect(patch.start_local).toBe('2026-10-12T12:00');
      expect(patch.start_tz).toBe('');
      expect(patch.start_tz_source).toBe('floating');
    });

    it('confirms a zone picked for a floating flight', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [floatingFlight()] }));
      const zone = screen.getAllByRole('combobox', { name: /timezone/i })[0];
      await userEvent.type(zone, 'Europe/Paris');
      const patch = await save();
      expect(patch.start_tz).toBe('Europe/Paris');
      expect(patch.start_tz_source).toBe('confirmed');
    });

    it('sends inferred, never unknown, with a zone it did not change', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(
        plan({
          parts: [
            part({
              type: 'dining',
              ends_at: undefined,
              start_tz_source: 'unknown',
              start: block('2026-10-12T11:35:00Z', 'Europe/London', { tz_source: 'unknown' }),
            }),
          ],
        }),
      );
      fireEvent.change(screen.getAllByLabelText('Date & time')[0], {
        target: { value: '2026-10-12T13:00' },
      });
      const patch = await save();
      expect(patch.start_tz).toBe('Europe/London');
      expect(patch.start_tz_source).toBe('inferred');
    });

    it('reports a time the server refuses and keeps the dialog open', async () => {
      h.updatePlanPart.mockRejectedValue(
        new Error("That time doesn't exist in Europe/London (clocks go forward)."),
      );
      render_(plan({ parts: [part()] }));
      fireEvent.change(screen.getAllByLabelText('Date & time')[0], {
        target: { value: '2026-03-29T01:30' },
      });
      await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
      await waitFor(() =>
        expect(h.setError).toHaveBeenCalledWith(
          "That time doesn't exist in Europe/London (clocks go forward).",
        ),
      );
      expect(screen.getByRole('button', { name: /^save$/i })).toBeInTheDocument();
    });

    it('moves a time the zone skips forward to the first real minute', async () => {
      h.updatePlanPart.mockResolvedValue(part());
      render_(plan({ parts: [part()] }));
      // 01:30 does not exist in London on 29 March 2026: the clocks go from
      // 01:00 to 02:00. Luxon, under the picker, reads it as 02:30 BST, so the
      // field and the patch both carry the time that does exist.
      const when = screen.getAllByLabelText('Date & time')[0];
      fireEvent.change(when, { target: { value: '2026-03-29T01:30' } });
      expect(when).toHaveValue('2026-03-29T02:30');
      const patch = await save();
      expect(patch.start_local).toBe('2026-03-29T02:30');
      expect(patch.start_tz).toBe('Europe/London');
      expect(patch.starts_at).toBeUndefined();
    });
  });
});
