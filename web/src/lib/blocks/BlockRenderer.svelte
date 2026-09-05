<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { registry } from './registry';
  import UnknownBlock from './UnknownBlock.svelte';

  type AnyBlock = WidgetDocument['blocks'][number];
  let { block }: { block: AnyBlock } = $props();

  const Component = $derived(registry[block.type]);
</script>

{#if Component}
  <Component {block} />
{:else}
  <UnknownBlock type={block.type} />
{/if}
