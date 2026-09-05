// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import TextBlock from './TextBlock.svelte';

describe('TextBlock', () => {
  it('plain "text" content is never parsed as markdown', () => {
    render(TextBlock, { props: { block: { type: 'text', content: '*not emphasis*' } } });
    expect(screen.getByText('*not emphasis*')).toBeInTheDocument();
  });

  it('markdown emphasis renders as a real <strong> element', () => {
    const { container } = render(TextBlock, {
      props: { block: { type: 'markdown', content: '**bold text**' } }
    });
    expect(container.querySelector('strong')?.textContent).toBe('bold text');
  });

  it('markdown links render as real anchors with the href set', () => {
    const { container } = render(TextBlock, {
      props: { block: { type: 'markdown', content: '[docs](https://example.com)' } }
    });
    const a = container.querySelector('a');
    expect(a?.getAttribute('href')).toBe('https://example.com');
  });

  it('a javascript: markdown link never becomes a real anchor', () => {
    const { container } = render(TextBlock, {
      props: { block: { type: 'markdown', content: '[click](javascript:alert(1))' } }
    });
    expect(container.querySelector('a')).toBeNull();
  });

  it('a markdown list renders as a real <ul>/<li>', () => {
    const { container } = render(TextBlock, {
      props: { block: { type: 'markdown', content: '- one\n- two' } }
    });
    expect(container.querySelectorAll('li')).toHaveLength(2);
  });

  it('never emits an {@html}-style raw HTML sink: markdown content containing an HTML-looking string renders as literal text', () => {
    // internal/widgets.Validate already rejects raw HTML server-side; this is the
    // frontend-side belt-and-braces check that nothing here interprets it either.
    const { container } = render(TextBlock, {
      props: { block: { type: 'markdown', content: 'plain <b>not bold</b> text' } }
    });
    expect(container.querySelector('b')).toBeNull();
    expect(container.textContent).toContain('<b>not bold</b>');
  });
});
