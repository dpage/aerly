import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => ({
  optInPlanAlerts: vi.fn(),
  optOutPlanAlerts: vi.fn(),
  setError: vi.fn(),
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      optInPlanAlerts: h.optInPlanAlerts,
      optOutPlanAlerts: h.optOutPlanAlerts,
      setError: h.setError,
    }),
}));

import PlanAlertToggle from './PlanAlertToggle';

beforeEach(() => {
  vi.clearAllMocks();
  h.optInPlanAlerts.mockResolvedValue(undefined);
  h.optOutPlanAlerts.mockResolvedValue(undefined);
});

describe('PlanAlertToggle', () => {
  it('reflects the initial opted-in state', () => {
    render(<PlanAlertToggle planId={5} optedIn />);
    expect(screen.getByRole('checkbox', { name: /notify me of changes/i })).toBeChecked();
  });

  it('opts in when toggled on', async () => {
    const onChange = vi.fn();
    render(<PlanAlertToggle planId={5} optedIn={false} onChange={onChange} />);
    await userEvent.click(screen.getByRole('checkbox', { name: /notify me of changes/i }));
    await waitFor(() => expect(h.optInPlanAlerts).toHaveBeenCalledWith(5));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('opts out when toggled off', async () => {
    const onChange = vi.fn();
    render(<PlanAlertToggle planId={9} optedIn onChange={onChange} />);
    await userEvent.click(screen.getByRole('checkbox', { name: /notify me of changes/i }));
    await waitFor(() => expect(h.optOutPlanAlerts).toHaveBeenCalledWith(9));
    expect(onChange).toHaveBeenCalledWith(false);
  });

  it('reverts and reports an error when opt-in fails', async () => {
    h.optInPlanAlerts.mockRejectedValue(new Error('opt boom'));
    render(<PlanAlertToggle planId={5} optedIn={false} />);
    const toggle = screen.getByRole('checkbox', { name: /notify me of changes/i });
    await userEvent.click(toggle);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('opt boom'));
    expect(toggle).not.toBeChecked();
  });

  it('stringifies non-Error rejections', async () => {
    h.optOutPlanAlerts.mockRejectedValue('kaboom');
    render(<PlanAlertToggle planId={5} optedIn />);
    await userEvent.click(screen.getByRole('checkbox', { name: /notify me of changes/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('kaboom'));
  });
});
