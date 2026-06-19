import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Attachment } from '../api/types';

const h = vi.hoisted(() => ({
  uploadPlanAttachment: vi.fn(),
  deleteAttachment: vi.fn(),
  downloadAttachment: vi.fn(),
  setError: vi.fn(),
  setNotice: vi.fn(),
  caps: {
    attachments_enabled: true,
    attachments_max_bytes: 1000,
  } as Record<string, unknown>,
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      capabilities: h.caps,
      setError: h.setError,
      setNotice: h.setNotice,
    }),
}));

vi.mock('../api/client', () => ({
  api: {
    uploadPlanAttachment: h.uploadPlanAttachment,
    deleteAttachment: h.deleteAttachment,
    downloadAttachment: h.downloadAttachment,
  },
}));

import PlanAttachments from './PlanAttachments';

function att(over: Partial<Attachment> = {}): Attachment {
  return {
    id: 1,
    plan_id: 7,
    filename: 'ticket.pdf',
    content_type: 'application/pdf',
    size_bytes: 2048,
    created_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.caps = { attachments_enabled: true, attachments_max_bytes: 1000 };
});

describe('PlanAttachments', () => {
  it('renders nothing when the feature is disabled', () => {
    h.caps = { attachments_enabled: false };
    const { container } = render(<PlanAttachments planId={7} attachments={[att()]} readOnly={false} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows an empty hint when there are no attachments', () => {
    render(<PlanAttachments planId={7} attachments={[]} readOnly={false} />);
    expect(screen.getByText(/no attachments yet/i)).toBeInTheDocument();
  });

  it('shows seeded attachments and downloads on click', async () => {
    render(<PlanAttachments planId={7} attachments={[att({ filename: 'voucher.png' })]} readOnly={false} />);
    const link = screen.getByRole('button', { name: 'voucher.png' });
    await userEvent.click(link);
    expect(h.downloadAttachment).toHaveBeenCalledTimes(1);
  });

  it('uploads a chosen file and prepends it to the list', async () => {
    const created = att({ id: 99, filename: 'new.pdf' });
    h.uploadPlanAttachment.mockResolvedValue(created);
    render(<PlanAttachments planId={7} attachments={[]} readOnly={false} />);

    const file = new File(['hello'], 'new.pdf', { type: 'application/pdf' });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, file);

    await waitFor(() => expect(h.uploadPlanAttachment).toHaveBeenCalledWith(7, file));
    expect(await screen.findByText('new.pdf')).toBeInTheDocument();
    expect(h.setNotice).toHaveBeenCalled();
  });

  it('rejects an oversize file before uploading', async () => {
    h.caps = { attachments_enabled: true, attachments_max_bytes: 3 };
    render(<PlanAttachments planId={7} attachments={[]} readOnly={false} />);
    const big = new File(['way too big'], 'big.bin');
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, big);
    expect(h.uploadPlanAttachment).not.toHaveBeenCalled();
    expect(h.setError).toHaveBeenCalled();
  });

  it('removes an attachment', async () => {
    h.deleteAttachment.mockResolvedValue(undefined);
    render(<PlanAttachments planId={7} attachments={[att({ id: 5, filename: 'gone.pdf' })]} readOnly={false} />);
    await userEvent.click(screen.getByRole('button', { name: /remove/i }));
    await waitFor(() => expect(h.deleteAttachment).toHaveBeenCalledWith(5));
    await waitFor(() => expect(screen.queryByText('gone.pdf')).not.toBeInTheDocument());
  });

  it('hides upload/remove controls when read-only but still allows download', () => {
    render(<PlanAttachments planId={7} attachments={[att({ filename: 'r.pdf' })]} readOnly={true} />);
    expect(screen.queryByRole('button', { name: /add file/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /remove/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'r.pdf' })).toBeInTheDocument();
  });

  it('surfaces an upload error', async () => {
    h.uploadPlanAttachment.mockRejectedValue(new Error('boom'));
    render(<PlanAttachments planId={7} attachments={[]} readOnly={false} />);
    const file = new File(['x'], 'x.pdf');
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, file);
    await waitFor(() => expect(h.setError).toHaveBeenCalled());
  });

  it('surfaces a download error', async () => {
    h.downloadAttachment.mockRejectedValue(new Error('nope'));
    render(<PlanAttachments planId={7} attachments={[att({ filename: 'd.pdf' })]} readOnly={false} />);
    await userEvent.click(screen.getByRole('button', { name: 'd.pdf' }));
    await waitFor(() => expect(h.setError).toHaveBeenCalled());
  });
});
