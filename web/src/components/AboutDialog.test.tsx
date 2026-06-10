import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { AdminInfo } from '../api/types';

const h = vi.hoisted(() => ({
  api: { getAdminInfo: vi.fn() },
}));

vi.mock('../api/client', () => ({ api: h.api }));

import AboutDialog from './AboutDialog';

function info(over: Partial<AdminInfo> = {}): AdminInfo {
  return {
    version: {
      commit: '0123456789abcdef0123456789abcdef01234567',
      short: '0123456789ab',
      modified: false,
      build_time: '2026-06-08T12:00:00Z',
      go_version: 'go1.26.1',
      os: 'linux',
      arch: 'amd64',
      ...over.version,
    },
    runtime: {
      started_at: '2026-06-08T10:00:00Z',
      uptime_sec: 3661,
      goroutines: 42,
      num_cpu: 8,
      ...over.runtime,
    },
    config: {
      public_url: 'https://aerly.example',
      tracker: 'opensky',
      tracker_authed: true,
      resolver_available: true,
      poll_interval_sec: 60,
      email_ingest_enabled: true,
      email_ingest_address: 'trips@aerly.example',
      llm_configured: true,
      llm_provider: 'anthropic',
      llm_model: 'claude-haiku-4-5',
      mail_configured: true,
      dev_auth_bypass: false,
      auth_github: true,
      auth_google: true,
      ...over.config,
    },
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.api.getAdminInfo.mockResolvedValue(info());
});

describe('AboutDialog', () => {
  it('does not render when closed and does not fetch', () => {
    render(<AboutDialog open={false} onClose={() => {}} />);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.api.getAdminInfo).not.toHaveBeenCalled();
  });

  it('fetches and renders the build, runtime and config sections when opened', async () => {
    render(<AboutDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.getAdminInfo).toHaveBeenCalled());
    expect(screen.getByText('0123456789ab')).toBeInTheDocument();
    expect(screen.getByText('go1.26.1')).toBeInTheDocument();
    expect(screen.getByText('linux/amd64')).toBeInTheDocument();
    expect(screen.getByText('1h 1m')).toBeInTheDocument(); // 3661s
    expect(screen.getByText('42')).toBeInTheDocument();
    expect(screen.getByText('OpenSky (authenticated)')).toBeInTheDocument();
    expect(screen.getByText('AeroDataBox')).toBeInTheDocument();
    expect(screen.getByText('anthropic / claude-haiku-4-5')).toBeInTheDocument();
    expect(screen.getByText('trips@aerly.example')).toBeInTheDocument();
    expect(screen.getByText('GitHub, Google')).toBeInTheDocument();
    // No dirty chip and no dev-bypass chip in the default fixture.
    expect(screen.queryByText('dirty')).not.toBeInTheDocument();
    expect(screen.queryByText('ON')).not.toBeInTheDocument();
  });

  it('shows an error alert when the fetch fails', async () => {
    h.api.getAdminInfo.mockRejectedValueOnce(new Error('boom'));
    render(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('boom');
  });

  it('renders a dirty chip and dev-bypass chip when applicable', async () => {
    h.api.getAdminInfo.mockResolvedValue(
      info({
        version: { modified: true } as AdminInfo['version'],
        config: { dev_auth_bypass: true } as AdminInfo['config'],
      }),
    );
    render(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByText('dirty')).toBeInTheDocument();
    expect(screen.getByText('ON')).toBeInTheDocument();
  });

  it('falls back gracefully when the commit and integrations are absent', async () => {
    h.api.getAdminInfo.mockResolvedValue(
      info({
        version: { commit: '', short: '', build_time: '' } as AdminInfo['version'],
        config: {
          tracker: 'stub',
          tracker_authed: false,
          resolver_available: false,
          email_ingest_enabled: false,
          llm_configured: false,
          mail_configured: false,
          auth_github: false,
          auth_google: false,
        } as AdminInfo['config'],
      }),
    );
    render(<AboutDialog open onClose={() => {}} />);
    await screen.findByText('stub');
    expect(screen.getByText('Commit').parentElement).toHaveTextContent('unknown');
    // 'disabled' appears for both email ingest and outbound mail.
    expect(screen.getAllByText('disabled').length).toBeGreaterThanOrEqual(2);
    // 'none' appears for flight data, LLM and sign-in.
    expect(screen.getAllByText('none').length).toBeGreaterThanOrEqual(2);
  });

  it('labels anonymous OpenSky and a single sign-in provider', async () => {
    h.api.getAdminInfo.mockResolvedValue(
      info({
        config: {
          tracker_authed: false,
          auth_github: true,
          auth_google: false,
        } as AdminInfo['config'],
      }),
    );
    render(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByText('OpenSky (anonymous)')).toBeInTheDocument();
    expect(screen.getByText('GitHub')).toBeInTheDocument();
  });

  it('shows "enabled" for email ingest with no address', async () => {
    h.api.getAdminInfo.mockResolvedValue(
      info({
        config: {
          email_ingest_enabled: true,
          email_ingest_address: undefined,
        } as AdminInfo['config'],
      }),
    );
    render(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByText('enabled')).toBeInTheDocument();
  });

  it('formats multi-day uptime and sub-minute uptime', async () => {
    h.api.getAdminInfo.mockResolvedValueOnce(
      info({ runtime: { uptime_sec: 90061 } as AdminInfo['runtime'] }),
    );
    const { rerender } = render(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByText('1d 1h 1m')).toBeInTheDocument();

    rerender(<AboutDialog open={false} onClose={() => {}} />);
    h.api.getAdminInfo.mockResolvedValueOnce(
      info({ runtime: { uptime_sec: 5 } as AdminInfo['runtime'] }),
    );
    rerender(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByText('5s')).toBeInTheDocument();
  });

  it('shows "unknown" for an empty build time and echoes an unparseable one', async () => {
    h.api.getAdminInfo.mockResolvedValueOnce(
      info({ version: { build_time: '' } as AdminInfo['version'] }),
    );
    const { rerender } = render(<AboutDialog open onClose={() => {}} />);
    await screen.findByText('go1.26.1');
    expect(screen.getByText('Built').parentElement).toHaveTextContent('unknown');

    rerender(<AboutDialog open={false} onClose={() => {}} />);
    h.api.getAdminInfo.mockResolvedValueOnce(
      info({ version: { build_time: 'not-a-date' } as AdminInfo['version'] }),
    );
    rerender(<AboutDialog open onClose={() => {}} />);
    expect(await screen.findByText('not-a-date')).toBeInTheDocument();
  });

  it('copies the full commit hash to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    render(<AboutDialog open onClose={() => {}} />);
    await screen.findByText('0123456789ab');
    await userEvent.click(screen.getByRole('button', { name: /copy commit hash/i }));
    expect(writeText).toHaveBeenCalledWith('0123456789abcdef0123456789abcdef01234567');
  });

  it('ignores a clipboard write rejection', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    Object.assign(navigator, { clipboard: { writeText } });
    render(<AboutDialog open onClose={() => {}} />);
    await screen.findByText('0123456789ab');
    await userEvent.click(screen.getByRole('button', { name: /copy commit hash/i }));
    expect(writeText).toHaveBeenCalled();
  });

  it('re-fetches when reopened', async () => {
    const { rerender } = render(<AboutDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.getAdminInfo).toHaveBeenCalledTimes(1));
    rerender(<AboutDialog open={false} onClose={() => {}} />);
    rerender(<AboutDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.api.getAdminInfo).toHaveBeenCalledTimes(2));
  });
});
