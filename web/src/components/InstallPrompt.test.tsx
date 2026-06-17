import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { InstallPrompt as InstallPromptState } from '../pwa';

const mock = vi.hoisted(() => ({
  state: { canInstall: false, iosHint: false, promptInstall: vi.fn() } as InstallPromptState,
}));

vi.mock('../pwa', () => ({ useInstallPrompt: () => mock.state }));

import InstallPrompt from './InstallPrompt';

beforeEach(() => {
  mock.state = { canInstall: false, iosHint: false, promptInstall: vi.fn() };
});

describe('InstallPrompt', () => {
  it('renders nothing on an unsupported/installed browser', () => {
    const { container } = render(<InstallPrompt />);
    expect(container).toBeEmptyDOMElement();
  });

  it('offers the native install prompt and triggers it', async () => {
    mock.state = { canInstall: true, iosHint: false, promptInstall: vi.fn() };
    render(<InstallPrompt />);
    expect(screen.getByText('Install Aerly on your device.')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Install' }));
    expect(mock.state.promptInstall).toHaveBeenCalled();
    // Dismissed after acting on it.
    expect(screen.queryByText('Install Aerly on your device.')).not.toBeInTheDocument();
  });

  it('can be dismissed with Later', async () => {
    mock.state = { canInstall: true, iosHint: false, promptInstall: vi.fn() };
    render(<InstallPrompt />);
    await userEvent.click(screen.getByRole('button', { name: 'Later' }));
    expect(screen.queryByText('Install Aerly on your device.')).not.toBeInTheDocument();
    expect(mock.state.promptInstall).not.toHaveBeenCalled();
  });

  it('shows the iOS Add-to-Home-Screen hint and can be closed', async () => {
    mock.state = { canInstall: false, iosHint: true, promptInstall: vi.fn() };
    render(<InstallPrompt />);
    expect(screen.getByText(/Add to Home Screen/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(screen.queryByText(/Add to Home Screen/)).not.toBeInTheDocument();
  });
});
