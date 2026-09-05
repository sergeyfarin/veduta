<script lang="ts">
  import type { InlineNode } from '../markdown';
  import Self from './InlineMarkdown.svelte';

  // Recursive by construction: a link or emphasis span may itself contain further emphasis. This
  // is the ONLY place InlineNode is turned into markup, and it is real Svelte elements the whole
  // way down - never {@html}. See markdown.ts for why link hrefs are pre-validated there rather
  // than trusted here.
  interface Props {
    nodes: InlineNode[];
  }
  let { nodes }: Props = $props();
</script>

{#each nodes as node (node)}
  {#if node.kind === 'text'}{node.value}{:else if node.kind === 'bold'}<strong
      ><Self nodes={node.children} /></strong
    >{:else if node.kind === 'italic'}<em><Self nodes={node.children} /></em
    >{:else if node.kind === 'code'}<code>{node.value}</code
    >{:else if node.kind === 'link'}<a href={node.href} target="_blank" rel="noopener noreferrer"
      ><Self nodes={node.children} /></a
    >{/if}
{/each}
