import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => {
  return {
    connectSSE: vi.fn((_handlers: { onFlight: (f: unknown) => void; onDelete: (id: number) => void }) => vi.fn()),
    state: {
      auth: 'loading' as 'loading' | 'anonymous' | 'authenticated',
      error: null as string | null,
      init: vi.fn(),
      setError: vi.fn(),
      applyFlightUpdate: vi.fn(),
      applyFlightDelete: vi.fn(),
    },
  };
});
const connectSSE = h.connectSSE;
const state = h.state;

vi.mock('./sse', () => ({ connectSSE: h.connectSSE }));
vi.mock('./components/AppShell', () => ({ default: () => <div>APP_SHELL</div> }));
vi.mock('./components/Login', () => ({ default: () => <div>LOGIN</div> }));
vi.mock('./components/PrivacyPolicy', () => ({ default: () => <div>PRIVACY_POLICY</div> }));
vi.mock('./components/TermsOfService', () => ({ default: () => <div>TERMS_OF_SERVICE</div> }));
vi.mock('./state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import App from './App';

beforeEach(() => {
  vi.clearAllMocks();
  state.auth = 'loading';
  state.error = null;
  window.history.pushState({}, '', '/');
});

describe('App', () => {
  it('shows a spinner while loading and calls init', () => {
    state.auth = 'loading';
    render(<App />);
    expect(document.querySelector('.MuiCircularProgress-root')).toBeTruthy();
    expect(state.init).toHaveBeenCalled();
  });

  it('renders Login when anonymous', () => {
    state.auth = 'anonymous';
    render(<App />);
    expect(screen.getByText('LOGIN')).toBeInTheDocument();
    expect(connectSSE).not.toHaveBeenCalled();
  });

  it('renders AppShell and wires SSE when authenticated', () => {
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('APP_SHELL')).toBeInTheDocument();
    expect(connectSSE).toHaveBeenCalledTimes(1);
    // The SSE handlers should forward to applyFlightUpdate / applyFlightDelete.
    const handlers = connectSSE.mock.calls[0][0];
    handlers.onFlight({ id: 7 });
    expect(state.applyFlightUpdate).toHaveBeenCalledWith({ id: 7 });
    handlers.onDelete(7);
    expect(state.applyFlightDelete).toHaveBeenCalledWith(7);
  });

  it('shows an error snackbar and clears it via the Alert close button', async () => {
    state.auth = 'anonymous';
    state.error = 'boom';
    render(<App />);
    expect(screen.getByText('boom')).toBeInTheDocument();
    const closeBtn = screen.getByRole('button', { name: /close/i });
    await userEvent.click(closeBtn);
    expect(state.setError).toHaveBeenCalledWith(null);
  });

  it('renders PrivacyPolicy at /privacy without waiting for auth', () => {
    window.history.pushState({}, '', '/privacy');
    state.auth = 'loading';
    render(<App />);
    expect(screen.getByText('PRIVACY_POLICY')).toBeInTheDocument();
    expect(document.querySelector('.MuiCircularProgress-root')).toBeNull();
  });

  it('renders TermsOfService at /terms without waiting for auth', () => {
    window.history.pushState({}, '', '/terms');
    state.auth = 'loading';
    render(<App />);
    expect(screen.getByText('TERMS_OF_SERVICE')).toBeInTheDocument();
    expect(document.querySelector('.MuiCircularProgress-root')).toBeNull();
  });

  it('renders PrivacyPolicy at /privacy even when authenticated', () => {
    window.history.pushState({}, '', '/privacy');
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('PRIVACY_POLICY')).toBeInTheDocument();
    expect(screen.queryByText('APP_SHELL')).not.toBeInTheDocument();
  });

  it('renders TermsOfService at /terms even when authenticated', () => {
    window.history.pushState({}, '', '/terms');
    state.auth = 'authenticated';
    render(<App />);
    expect(screen.getByText('TERMS_OF_SERVICE')).toBeInTheDocument();
    expect(screen.queryByText('APP_SHELL')).not.toBeInTheDocument();
  });

  it('Snackbar onClose fires setError(null) on autohide timeout', async () => {
    vi.useFakeTimers();
    state.auth = 'anonymous';
    state.error = 'temp';
    render(<App />);
    await act(async () => {
      // MUI Snackbar autoHideDuration is 6000ms; advance past it.
      vi.advanceTimersByTime(6500);
    });
    vi.useRealTimers();
    expect(state.setError).toHaveBeenCalledWith(null);
  });
});
