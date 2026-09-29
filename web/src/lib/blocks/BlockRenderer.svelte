<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import type { CardHistory } from '../types/cardstate';
  import { registry } from './registry';
  import UnknownBlock from './UnknownBlock.svelte';

  type AnyBlock = WidgetDocument['blocks'][number];
  // history is the card state's core-owned retained points. Every block is handed it and only a
  // bound series block reads it: blocks never fetch anything themselves.
  let { block, history }: { block: AnyBlock; history?: CardHistory } = $props();

  const Component = $derived(registry[block.type]);
</script>

{#if Component}
  <Component {block} {history} />
{:else}
  <UnknownBlock type={block.type} />
{/if}
