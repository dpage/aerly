import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import PlanLinksEditor from './PlanLinksEditor';

const row = { title: 'Ticket', url: 'https://example.com/t.pdf' };

describe('PlanLinksEditor', () => {
  it('shows a hint and an Add link button when there are none', async () => {
    const onChange = vi.fn();
    render(<PlanLinksEditor links={[]} onChange={onChange} readOnly={false} />);
    expect(screen.getByText(/Link to tickets/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add link' }));
    expect(onChange).toHaveBeenCalledWith([{ title: '', url: '' }]);
  });

  it('edits the title and URL of a row', () => {
    const onChange = vi.fn();
    render(<PlanLinksEditor links={[row, row]} onChange={onChange} readOnly={false} />);
    fireEvent.change(screen.getAllByLabelText('Title')[1], { target: { value: 'Pass' } });
    expect(onChange).toHaveBeenLastCalledWith([row, { ...row, title: 'Pass' }]);
    fireEvent.change(screen.getAllByLabelText('URL')[0], {
      target: { value: 'https://example.com/p.png' },
    });
    expect(onChange).toHaveBeenLastCalledWith([{ ...row, url: 'https://example.com/p.png' }, row]);
  });

  it('removes a row', async () => {
    const onChange = vi.fn();
    const other = { title: 'Pass', url: 'https://example.com/p.png' };
    render(<PlanLinksEditor links={[row, other]} onChange={onChange} readOnly={false} />);
    await userEvent.click(screen.getByRole('button', { name: 'Remove link 1' }));
    expect(onChange).toHaveBeenCalledWith([other]);
  });

  it('is read-only offline', () => {
    render(<PlanLinksEditor links={[row]} onChange={vi.fn()} readOnly />);
    expect(screen.getByLabelText('Title')).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Add link' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Remove link/ })).not.toBeInTheDocument();
  });
});
