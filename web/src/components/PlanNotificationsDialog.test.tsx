import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Plan } from '../api/types';

vi.mock('./PlanAlertToggle', () => ({ default: () => <div data-testid="alert-toggle" /> }));
vi.mock('./PlanReminderOverride', () => ({ default: () => <div data-testid="reminder-override" /> }));

import PlanNotificationsDialog from './PlanNotificationsDialog';

const plan = {} as unknown as Plan;

describe('PlanNotificationsDialog', () => {
  it('shows the alert opt-in and the reminder override for a viewer', () => {
    render(<PlanNotificationsDialog open plan={plan} isViewer onClose={() => {}} />);
    expect(screen.getByTestId('alert-toggle')).toBeInTheDocument();
    expect(screen.getByTestId('reminder-override')).toBeInTheDocument();
  });

  it('hides the viewer-only alert opt-in for owners/editors', () => {
    render(<PlanNotificationsDialog open plan={plan} isViewer={false} onClose={() => {}} />);
    expect(screen.queryByTestId('alert-toggle')).not.toBeInTheDocument();
    expect(screen.getByTestId('reminder-override')).toBeInTheDocument();
  });

  it('renders nothing when closed', () => {
    render(<PlanNotificationsDialog open={false} plan={plan} isViewer onClose={() => {}} />);
    expect(screen.queryByTestId('reminder-override')).not.toBeInTheDocument();
  });

  it('calls onClose from the Close button', async () => {
    const onClose = vi.fn();
    render(<PlanNotificationsDialog open plan={plan} isViewer onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalled();
  });
});
