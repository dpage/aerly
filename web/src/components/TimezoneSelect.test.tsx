import { useState } from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import TimezoneSelect from './TimezoneSelect';

/** Stateful wrapper: the component is controlled, so a real test needs to feed
 * each reported value back in (mirroring how PlanEditDialog holds the tz). */
function Harness({ initial = '', onChange }: { initial?: string; onChange?: (v: string) => void }) {
  const [tz, setTz] = useState(initial);
  return (
    <TimezoneSelect
      value={tz}
      onChange={(v) => {
        setTz(v);
        onChange?.(v);
      }}
    />
  );
}

describe('TimezoneSelect', () => {
  it('matches an unanchored, case-insensitive substring ("van" → Vancouver)', async () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const input = screen.getByRole('combobox', { name: /timezone/i });

    await userEvent.type(input, 'van');

    // The typed text round-trips up to the caller.
    expect(onChange).toHaveBeenLastCalledWith('van');
    // The dropdown surfaces Vancouver even though "van" is mid-string.
    const list = screen.getByRole('listbox');
    expect(within(list).getByText('America/Vancouver')).toBeInTheDocument();
  });

  it('selecting an option reports the full IANA name', async () => {
    const onChange = vi.fn();
    render(<Harness initial="van" onChange={onChange} />);
    const input = screen.getByRole('combobox', { name: /timezone/i });
    await userEvent.click(input);

    const option = await screen.findByText('America/Vancouver');
    await userEvent.click(option);

    expect(onChange).toHaveBeenLastCalledWith('America/Vancouver');
  });

  it('matches across the IANA separator ("new york" → America/New_York)', async () => {
    render(<Harness />);
    const input = screen.getByRole('combobox', { name: /timezone/i });

    await userEvent.type(input, 'new york');

    const list = screen.getByRole('listbox');
    expect(within(list).getByText('America/New_York')).toBeInTheDocument();
  });

  it('calls onCommit on Enter and blur, but not per keystroke', async () => {
    const onCommit = vi.fn();
    render(<TimezoneSelect value="" onChange={() => {}} onCommit={onCommit} />);
    const input = screen.getByRole('combobox');
    await userEvent.type(input, 'x');
    expect(onCommit).not.toHaveBeenCalled();
    await userEvent.tab();
    expect(onCommit).toHaveBeenCalledTimes(1);
  });

  it('calls onCommit with the chosen option', async () => {
    const onCommit = vi.fn();
    function Wrap() {
      const [tz, setTz] = useState('');
      return <TimezoneSelect value={tz} onChange={setTz} onCommit={onCommit} />;
    }
    render(<Wrap />);
    await userEvent.type(screen.getByRole('combobox'), 'tashk');
    await userEvent.keyboard('{Enter}');
    expect(onCommit).toHaveBeenCalledWith('Asia/Tashkent');
  });

  it('does not commit when the text is emptied, only on the later blur', async () => {
    const onCommit = vi.fn();
    const onChange = vi.fn();
    render(<TimezoneSelect value="Asia/Tashkent" onChange={onChange} onCommit={onCommit} />);
    await userEvent.clear(screen.getByRole('combobox'));
    expect(onChange).toHaveBeenCalledWith('');
    expect(onCommit).not.toHaveBeenCalled();
  });
});
