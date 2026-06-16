import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

import type { NotificationItem } from '../api/types';

const h = vi.hoisted(() => ({
  loadAlerts: vi.fn().mockResolvedValue(undefined),
  markAlertsRead: vi.fn().mockResolvedValue(undefined),
  deleteAlert: vi.fn().mockResolvedValue(undefined),
  clearAlerts: vi.fn().mockResolvedValue(undefined),
  state: {
    alerts: [] as NotificationItem[],
    notifications: { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 },
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      alerts: h.state.alerts,
      notifications: h.state.notifications,
      loadAlerts: h.loadAlerts,
      markAlertsRead: h.markAlertsRead,
      deleteAlert: h.deleteAlert,
      clearAlerts: h.clearAlerts,
    }),
}));

import AlertsDialog from './AlertsDialog';

const onClose = vi.fn();

function alert(over: Partial<NotificationItem> = {}): NotificationItem {
  return {
    id: 1,
    source: 'flight',
    kind: 'gate',
    trip_id: 4,
    plan_id: 1,
    plan_part_id: 5,
    message: 'BA286 now departs gate B32',
    created_at: '2026-06-01T00:00:00Z',
    ...over,
  };
}

function renderDialog(open = true) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route path="/" element={<AlertsDialog open={open} onClose={onClose} />} />
        <Route path="/tracker" element={<div data-testid="page">tracker page</div>} />
        <Route path="/trips/:id" element={<div data-testid="page">trip page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.alerts = [];
  h.state.notifications = { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 };
});

describe('AlertsDialog', () => {
  it('loads fresh history when opened', () => {
    renderDialog();
    expect(h.loadAlerts).toHaveBeenCalled();
  });

  it('shows an empty state when there are no alerts', () => {
    renderDialog();
    expect(screen.getByText(/no alerts/i)).toBeInTheDocument();
  });

  it('lists alerts with their messages', () => {
    h.state.alerts = [alert(), alert({ id: 2, message: 'BA286 cancelled' })];
    renderDialog();
    expect(screen.getByText('BA286 now departs gate B32')).toBeInTheDocument();
    expect(screen.getByText('BA286 cancelled')).toBeInTheDocument();
  });

  it('deep-links to the tracker for a flight alert with a plan_part_id', async () => {
    h.state.alerts = [alert({ plan_part_id: 5 })];
    renderDialog();
    await userEvent.click(screen.getByText('BA286 now departs gate B32'));
    expect(screen.getByTestId('page')).toHaveTextContent('tracker page');
  });

  it('opens the trip timeline for an item without a plan_part_id', async () => {
    h.state.alerts = [
      alert({ kind: 'share', source: 'notification', plan_part_id: undefined, message: 'shared' }),
    ];
    renderDialog();
    await userEvent.click(screen.getByText('shared'));
    expect(screen.getByTestId('page')).toHaveTextContent('trip page');
  });

  it('deletes a single alert via its delete button', async () => {
    const al = alert({ id: 9 });
    h.state.alerts = [al];
    renderDialog();
    await userEvent.click(screen.getByRole('button', { name: /delete alert/i }));
    expect(h.deleteAlert).toHaveBeenCalledWith(al);
  });

  it('clears all alerts via the Clear all button', async () => {
    h.state.alerts = [alert()];
    renderDialog();
    await userEvent.click(screen.getByRole('button', { name: /clear all/i }));
    expect(h.clearAlerts).toHaveBeenCalled();
  });

  it('disables Clear all when the inbox is empty', () => {
    renderDialog();
    expect(screen.getByRole('button', { name: /clear all/i })).toBeDisabled();
  });

  it('marks alerts read when closed while some are unread', async () => {
    h.state.notifications = { friend_requests_pending: 0, unread_alerts: 1, unread_shares: 0 };
    h.state.alerts = [alert({ read_at: undefined })];
    renderDialog();
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(h.markAlertsRead).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('does not mark read on close when nothing is unread', async () => {
    h.state.notifications = { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 };
    h.state.alerts = [alert({ read_at: '2026-06-01T01:00:00Z' })];
    renderDialog();
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(h.markAlertsRead).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('flags unread items with an unread marker that read items lack', () => {
    h.state.alerts = [
      alert({ id: 1, message: 'unread one', read_at: undefined }),
      alert({ id: 2, message: 'read one', read_at: '2026-06-01T01:00:00Z' }),
    ];
    renderDialog();
    // The unread row renders its message in bolder type than the read row.
    const unread = screen.getByText('unread one');
    const read = screen.getByText('read one');
    expect(within(unread).queryByText('read one')).toBeNull();
    expect(unread).toHaveStyle({ fontWeight: '600' });
    expect(read).toHaveStyle({ fontWeight: '400' });
  });
});
