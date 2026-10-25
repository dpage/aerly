import { describe, it, expect } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import PlanLinks from './PlanLinks';

const pdf = { id: 1, title: 'E-ticket', url: 'https://example.com/ticket.pdf' };
const img = { id: 2, title: 'Boarding pass', url: 'https://example.com/pass.png' };

describe('PlanLinks', () => {
  it('renders nothing without links', () => {
    const { container } = render(<PlanLinks links={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('opens non-image links in a new tab', () => {
    render(<PlanLinks links={[pdf]} />);
    const a = screen.getByRole('link', { name: /E-ticket/ });
    expect(a).toHaveAttribute('href', pdf.url);
    expect(a).toHaveAttribute('target', '_blank');
    expect(a).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('shows an image link in a dialog, and closes it', async () => {
    render(<PlanLinks links={[img]} />);
    await userEvent.click(screen.getByRole('button', { name: /Boarding pass/ }));
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveTextContent('Boarding pass');
    expect(screen.getByRole('img', { name: 'Boarding pass' })).toHaveAttribute('src', img.url);
    expect(screen.getByRole('link', { name: /Open in new tab/ })).toHaveAttribute('href', img.url);
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('falls back to a message when the image will not load', async () => {
    render(<PlanLinks links={[img]} />);
    await userEvent.click(screen.getByRole('button', { name: /Boarding pass/ }));
    fireEvent.error(screen.getByRole('img', { name: 'Boarding pass' }));
    expect(screen.getByText(/couldn’t be shown here/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Open in new tab/ })).toBeInTheDocument();
  });
});
