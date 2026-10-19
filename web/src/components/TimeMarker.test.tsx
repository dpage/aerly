import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { PlanPart } from '../api/types';
import { block } from '../test/blocks';

const h = vi.hoisted(() => ({ updatePlanPart: vi.fn(), setError: vi.fn() }));
vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ updatePlanPart: h.updatePlanPart, setError: h.setError }),
}));

import TimeMarker from './TimeMarker';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 7, plan_id: 3, type: 'hotel', seq: 0, status: 'planned',
    starts_at: '2026-10-18T09:00:00Z', start_tz: 'Asia/Tashkent', start_tz_source: 'inferred',
    start_label: 'Test Hotel', effective_at: '2026-10-18T09:00:00Z',
    start: block('2026-10-18T09:00:00Z', 'Asia/Tashkent', { tz_source: 'inferred' }),
    ...over,
  } as PlanPart;
}

beforeEach(() => {
  h.updatePlanPart.mockReset();
  h.setError.mockReset();
});

describe('TimeMarker', () => {
  it('renders nothing for a confirmed scheduled time', () => {
    const { container } = render(
      <TimeMarker part={part({ start: block('2026-10-18T09:00:00Z', 'Asia/Tashkent') })} which="start" canEdit />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('confirms a guessed zone with a PATCH', async () => {
    h.updatePlanPart.mockResolvedValue(undefined);
    render(<TimeMarker part={part()} which="start" canEdit />);
    await userEvent.click(screen.getByRole('button', { name: 'zone guessed, tap to confirm' }));
    await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() =>
      expect(h.updatePlanPart).toHaveBeenCalledWith(7, {
        start_tz: 'Asia/Tashkent',
        start_tz_source: 'confirmed',
      }),
    );
  });

  it('opens the picker for a missing zone and reports a server refusal', async () => {
    h.updatePlanPart.mockRejectedValue(new Error("That time doesn't exist in Europe/London (clocks go forward)."));
    render(
      <TimeMarker
        part={part({
          starts_at: '2026-03-29T01:30:00Z',
          effective_at: '2026-03-29T01:30:00Z',
          start_tz: '',
          start_tz_source: 'floating',
          start: block('2026-03-29T01:30:00Z', ''),
        })}
        which="start"
        canEdit
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'needs a time zone' }));
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled();
    await userEvent.type(screen.getByRole('combobox', { name: /timezone/i }), 'Europe/London');
    await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() =>
      expect(h.setError).toHaveBeenCalledWith("That time doesn't exist in Europe/London (clocks go forward)."),
    );
    // The floating digits went as wall-clock time in the zone picked, which is
    // what the server refused, and the picker stays open to try another.
    expect(h.updatePlanPart).toHaveBeenCalledWith(7, {
      start_tz: 'Europe/London',
      start_tz_source: 'confirmed',
      start_local: '2026-03-29T01:30',
    });
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
  });

  it('cancels without a PATCH', async () => {
    render(<TimeMarker part={part()} which="start" canEdit />);
    await userEvent.click(screen.getByRole('button', { name: 'zone guessed, tap to confirm' }));
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(h.updatePlanPart).not.toHaveBeenCalled();
  });

  it('closes on Escape and does not let clicks inside the dialog reach the tile', async () => {
    const onTile = vi.fn();
    render(
      <div onClick={onTile}>
        <TimeMarker part={part()} which="start" canEdit />
      </div>,
    );
    await userEvent.click(screen.getByRole('button', { name: 'zone guessed, tap to confirm' }));
    expect(onTile).not.toHaveBeenCalled();
    await userEvent.click(screen.getByText('Confirm the time zone'));
    expect(onTile).not.toHaveBeenCalled();
    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByText('Confirm the time zone')).toBeNull());
    expect(h.updatePlanPart).not.toHaveBeenCalled();
  });

  it('is plain text for a viewer who cannot edit', () => {
    render(<TimeMarker part={part()} which="start" canEdit={false} />);
    expect(screen.getByText('zone guessed')).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('says a derived arrival is worked out from the delay', () => {
    const p = part({
      end: block('2026-10-18T13:00:00Z', 'Asia/Tashkent', { kind: 'derived', delay_min: 20 }),
    });
    render(<TimeMarker part={p} which="end" canEdit />);
    expect(screen.getByText('(based on departure delay)')).toBeInTheDocument();
  });

  it('renders nothing for an end that does not exist', () => {
    const { container } = render(<TimeMarker part={part()} which="end" canEdit />);
    expect(container).toBeEmptyDOMElement();
  });
});
