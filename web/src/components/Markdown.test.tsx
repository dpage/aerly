import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import Markdown from './Markdown';

describe('Markdown', () => {
  it('renders nothing for blank input', () => {
    const { container } = render(<Markdown text="   " />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders headings as the matching heading element', () => {
    render(<Markdown text={'# Big\n\n###### Small'} />);
    expect(screen.getByRole('heading', { level: 1, name: 'Big' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 6, name: 'Small' })).toBeInTheDocument();
  });

  it('renders bold, italic and inline code', () => {
    const { container } = render(<Markdown text={'**b** _i_ `c`'} />);
    expect(container.querySelector('strong')).toHaveTextContent('b');
    expect(container.querySelector('em')).toHaveTextContent('i');
    expect(container.querySelector('code')).toHaveTextContent('c');
  });

  it('renders a safe link that opens in a new tab', () => {
    render(<Markdown text="[album](https://photos.example.com)" />);
    const link = screen.getByRole('link', { name: 'album' });
    expect(link).toHaveAttribute('href', 'https://photos.example.com');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('does not render an anchor for an unsafe link', () => {
    render(<Markdown text="a [x](javascript:boom) b" />);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByText(/a x b/)).toBeInTheDocument();
  });

  it('renders unordered and ordered lists with their items', () => {
    render(<Markdown text={'- one\n- two\n\n1. a\n2. b'} />);
    const lists = screen.getAllByRole('list');
    expect(lists).toHaveLength(2);
    expect(screen.getAllByRole('listitem').map((li) => li.textContent)).toEqual([
      'one',
      'two',
      'a',
      'b',
    ]);
    expect(lists[0].tagName).toBe('UL');
    expect(lists[1].tagName).toBe('OL');
  });

  it('renders plain paragraph text, accepting an optional sx', () => {
    render(<Markdown text="just a note" sx={{ mt: 2 }} />);
    expect(screen.getByText('just a note')).toBeInTheDocument();
  });
});
