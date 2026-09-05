// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, it, expect } from 'vitest';
import { parseMarkdown, type InlineNode } from './markdown';

// Renders a parsed AST back to a plain string for assertions that only care about the text
// content, not the node shape.
function plainText(nodes: InlineNode[]): string {
  return nodes
    .map((n) => {
      switch (n.kind) {
        case 'text':
          return n.value;
        case 'code':
          return n.value;
        case 'bold':
        case 'italic':
          return plainText(n.children);
        case 'link':
          return plainText(n.children);
      }
    })
    .join('');
}

describe('parseMarkdown', () => {
  it('plain text becomes a single paragraph', () => {
    const blocks = parseMarkdown('hello world');
    expect(blocks).toEqual([{ kind: 'paragraph', children: [{ kind: 'text', value: 'hello world' }] }]);
  });

  it('bold', () => {
    const blocks = parseMarkdown('**bold**');
    expect(blocks[0]).toEqual({
      kind: 'paragraph',
      children: [{ kind: 'bold', children: [{ kind: 'text', value: 'bold' }] }]
    });
  });

  it('italic with underscores', () => {
    const blocks = parseMarkdown('_italic_');
    expect(blocks[0]).toEqual({
      kind: 'paragraph',
      children: [{ kind: 'italic', children: [{ kind: 'text', value: 'italic' }] }]
    });
  });

  it('inline code', () => {
    const blocks = parseMarkdown('`code`');
    expect(blocks[0]).toEqual({ kind: 'paragraph', children: [{ kind: 'code', value: 'code' }] });
  });

  it('a safe http(s) link becomes a real link node', () => {
    const blocks = parseMarkdown('[click here](https://example.com/x)');
    expect(blocks[0]).toEqual({
      kind: 'paragraph',
      children: [{ kind: 'link', href: 'https://example.com/x', children: [{ kind: 'text', value: 'click here' }] }]
    });
  });

  it('mixed inline content in one paragraph, in document order', () => {
    const blocks = parseMarkdown('plain **bold** and `code` and [a link](http://x)');
    expect(plainText((blocks[0] as any).children)).toBe('plain bold and code and a link');
  });

  it('an unordered list with "- "', () => {
    const blocks = parseMarkdown('- one\n- two\n- three');
    expect(blocks).toEqual([
      {
        kind: 'list',
        items: [
          [{ kind: 'text', value: 'one' }],
          [{ kind: 'text', value: 'two' }],
          [{ kind: 'text', value: 'three' }]
        ]
      }
    ]);
  });

  it('an unordered list with "* "', () => {
    const blocks = parseMarkdown('* one\n* two');
    expect(blocks[0]!.kind).toBe('list');
  });

  it('a blank line separates a paragraph from a following list', () => {
    const blocks = parseMarkdown('intro text\n\n- one\n- two');
    expect(blocks.map((b) => b.kind)).toEqual(['paragraph', 'list']);
  });

  it('a list followed by more paragraph text closes the list', () => {
    const blocks = parseMarkdown('- one\n- two\nmore text');
    expect(blocks.map((b) => b.kind)).toEqual(['list', 'paragraph']);
  });

  it('empty content produces no blocks', () => {
    expect(parseMarkdown('')).toEqual([]);
  });

  it('whitespace-only content produces no blocks', () => {
    expect(parseMarkdown('   \n\n  ')).toEqual([]);
  });

  describe('link scheme safety - the reason this parser exists rather than trusting markdown blindly', () => {
    it('a javascript: link is rendered as plain text, never as a link node', () => {
      const blocks = parseMarkdown('[click me](javascript:alert(1))');
      const children = (blocks[0] as any).children as InlineNode[];
      expect(children.some((n) => n.kind === 'link')).toBe(false);
      expect(plainText(children)).toContain('javascript:alert(1)');
    });

    it('a data: link is rendered as plain text', () => {
      const blocks = parseMarkdown('[x](data:text/html,<script>alert(1)</script>)');
      const children = (blocks[0] as any).children as InlineNode[];
      expect(children.some((n) => n.kind === 'link')).toBe(false);
    });

    it('a vbscript: link is rendered as plain text', () => {
      const blocks = parseMarkdown('[x](vbscript:msgbox(1))');
      const children = (blocks[0] as any).children as InlineNode[];
      expect(children.some((n) => n.kind === 'link')).toBe(false);
    });

    it('a schemeless target is rendered as plain text, not assumed relative-safe', () => {
      const blocks = parseMarkdown('[x](evil())');
      const children = (blocks[0] as any).children as InlineNode[];
      expect(children.some((n) => n.kind === 'link')).toBe(false);
    });

    it('http and https are both accepted', () => {
      expect(
        (parseMarkdown('[a](http://x)')[0] as any).children.some((n: InlineNode) => n.kind === 'link')
      ).toBe(true);
      expect(
        (parseMarkdown('[a](https://x)')[0] as any).children.some((n: InlineNode) => n.kind === 'link')
      ).toBe(true);
    });
  });

  it('never throws and never infinitely recurses on pathological input', () => {
    const inputs = [
      '*'.repeat(5000),
      '['.repeat(2000) + ']'.repeat(2000),
      '**'.repeat(1000),
      '`'.repeat(3000),
      '- '.repeat(1000)
    ];
    for (const input of inputs) {
      expect(() => parseMarkdown(input)).not.toThrow();
    }
  });

  it('deeply nested emphasis is bounded rather than recursing without limit', () => {
    const nested = '*'.repeat(20) + 'x' + '*'.repeat(20);
    expect(() => parseMarkdown(nested)).not.toThrow();
  });
});
