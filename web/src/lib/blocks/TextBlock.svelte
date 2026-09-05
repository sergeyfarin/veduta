<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { parseMarkdown } from '../markdown';
  import InlineMarkdown from './InlineMarkdown.svelte';

  type Block = Extract<WidgetDocument['blocks'][number], { type: 'text' | 'markdown' }>;
  let { block }: { block: Block } = $props();

  // Plain "text" content is never parsed as markdown - a literal "*" in a status message must
  // stay a literal "*", not turn into emphasis. Only "markdown"-kind blocks go through the
  // restricted-subset parser.
  const parsed = $derived(block.type === 'markdown' ? parseMarkdown(block.content) : null);
</script>

<div class="text" class:strong={block.emphasis === 'strong'} class:subtle={block.emphasis === 'subtle'}>
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#if parsed}
    {#each parsed as node, i (i)}
      {#if node.kind === 'paragraph'}
        <p><InlineMarkdown nodes={node.children} /></p>
      {:else}
        <ul>
          {#each node.items as item, j (j)}
            <li><InlineMarkdown nodes={item} /></li>
          {/each}
        </ul>
      {/if}
    {/each}
  {:else}
    <p>{block.content}</p>
  {/if}
</div>

<style>
  .text {
    font-size: 13px;
    color: var(--v-text);
  }
  .text.subtle {
    color: var(--v-muted);
  }
  .text.strong {
    font-weight: 600;
  }
  h4 {
    margin: 0 0 var(--v-s-2);
    font-size: 12px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  p {
    margin: 0 0 var(--v-s-2);
    line-height: 1.5;
  }
  p:last-child {
    margin-bottom: 0;
  }
  ul {
    margin: 0 0 var(--v-s-2);
    padding-left: 1.2em;
  }
  ul:last-child {
    margin-bottom: 0;
  }
  li {
    margin-bottom: 2px;
  }
  :global(.text code) {
    font-family: var(--v-font-num);
    font-size: 0.9em;
    background: var(--v-surface-2);
    padding: 1px 5px;
    border-radius: 4px;
  }
  :global(.text a) {
    color: var(--v-accent);
  }
</style>
