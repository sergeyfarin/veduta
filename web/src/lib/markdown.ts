// SPDX-License-Identifier: AGPL-3.0-or-later

// Parses the restricted markdown subset a "markdown"-kind text block may contain (emphasis,
// links, inline code, lists - schemas/widget-document.v1.schema.json's own description for
// blockText) into a small, inert AST that TextBlock.svelte renders as real Svelte elements.
//
// This is NOT a defensive sanitiser - internal/widgets.Validate already rejects any markdown
// content containing a raw HTML tag before it ever reaches the browser (docs/01-architecture.md
// section 8), so a hostile "<script>" never gets this far. What this module guards against is
// narrower and specific to markdown's OWN syntax: a link written as `[text](javascript:evil())`
// is not raw HTML and validate.go's regex does not (and should not have to) know markdown link
// syntax, so this parser is the thing that keeps a markdown LINK from becoming a real anchor
// pointed at a dangerous scheme. See the safeHref check below.
//
// Never produces an HTML string. Every node here is rendered by TextBlock.svelte as a real
// Svelte element tree - {@html} appears nowhere in this project, and a lint rule plus a CI grep
// enforce that (see B3 in docs/02-implementation-plan.md).

export type InlineNode =
  | { kind: 'text'; value: string }
  | { kind: 'bold'; children: InlineNode[] }
  | { kind: 'italic'; children: InlineNode[] }
  | { kind: 'code'; value: string }
  | { kind: 'link'; href: string; children: InlineNode[] };

export type BlockNode =
  | { kind: 'paragraph'; children: InlineNode[] }
  | { kind: 'list'; items: InlineNode[][] };

const MAX_INLINE_DEPTH = 8; // markdown content is already capped at 2KB server-side; this is a
// second, independent bound on recursion, not a length limit

/** True only for the schemes an <a href> may safely use. Anything else - javascript:, data:,
 * vbscript:, a bare "evil()" with no scheme that a browser could still misinterpret - is
 * rejected, and the parser falls back to rendering the link as plain bracketed text instead of a
 * clickable anchor, so the content is never silently dropped, just never turned into a link. */
function safeHref(href: string): boolean {
  return /^https?:\/\//i.test(href.trim());
}

function parseInline(text: string, depth = 0): InlineNode[] {
  if (depth >= MAX_INLINE_DEPTH || text.length === 0) {
    return text.length > 0 ? [{ kind: 'text', value: text }] : [];
  }

  // Order matters: code spans are matched first so a literal `*` or `_` inside a code span is
  // never mistaken for emphasis markup.
  const patterns: [RegExp, (m: RegExpMatchArray) => InlineNode][] = [
    [/`([^`]+)`/, (m) => ({ kind: 'code', value: m[1]! })],
    [
      /\[([^\]]+)\]\(([^)]+)\)/,
      (m) =>
        safeHref(m[2]!)
          ? { kind: 'link', href: m[2]!.trim(), children: parseInline(m[1]!, depth + 1) }
          : { kind: 'text', value: m[0] }
    ],
    [/\*\*([^*]+)\*\*/, (m) => ({ kind: 'bold', children: parseInline(m[1]!, depth + 1) })],
    [/__([^_]+)__/, (m) => ({ kind: 'bold', children: parseInline(m[1]!, depth + 1) })],
    [/\*([^*]+)\*/, (m) => ({ kind: 'italic', children: parseInline(m[1]!, depth + 1) })],
    [/_([^_]+)_/, (m) => ({ kind: 'italic', children: parseInline(m[1]!, depth + 1) })]
  ];

  let earliest: { index: number; match: RegExpMatchArray; build: (m: RegExpMatchArray) => InlineNode } | null =
    null;
  for (const [re, build] of patterns) {
    const m = text.match(re);
    if (m && m.index !== undefined && (earliest === null || m.index < earliest.index)) {
      earliest = { index: m.index, match: m, build };
    }
  }

  if (!earliest) return [{ kind: 'text', value: text }];

  const before = text.slice(0, earliest.index);
  const after = text.slice(earliest.index + earliest.match[0].length);
  const nodes: InlineNode[] = [];
  if (before) nodes.push({ kind: 'text', value: before });
  nodes.push(earliest.build(earliest.match));
  nodes.push(...parseInline(after, depth));
  return nodes;
}

/** Parses restricted markdown into block-level nodes: paragraphs (blank-line separated) and
 * unordered lists (lines starting with "- " or "* "). No headings, tables, blockquotes, ordered
 * lists, or nested lists - anything beyond the schema's stated subset renders as plain paragraph
 * text rather than being silently dropped. */
export function parseMarkdown(content: string): BlockNode[] {
  const lines = content.split('\n');
  const blocks: BlockNode[] = [];
  let paragraphLines: string[] = [];
  let listItems: InlineNode[][] = [];

  const flushParagraph = () => {
    if (paragraphLines.length > 0) {
      blocks.push({ kind: 'paragraph', children: parseInline(paragraphLines.join(' ').trim()) });
      paragraphLines = [];
    }
  };
  const flushList = () => {
    if (listItems.length > 0) {
      blocks.push({ kind: 'list', items: listItems });
      listItems = [];
    }
  };

  for (const rawLine of lines) {
    const line = rawLine.trim();
    const listMatch = line.match(/^[-*]\s+(.*)$/);
    if (listMatch) {
      flushParagraph();
      listItems.push(parseInline(listMatch[1]!));
    } else if (line === '') {
      flushParagraph();
      flushList();
    } else {
      flushList();
      paragraphLines.push(line);
    }
  }
  flushParagraph();
  flushList();
  return blocks;
}
