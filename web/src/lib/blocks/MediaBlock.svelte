<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import MediaTile from './MediaTile.svelte';

  type Block = Extract<WidgetDocument['blocks'][number], { type: 'image' | 'image-grid' | 'poster-grid' }>;
  let { block }: { block: Block } = $props();

  // Per-kind defaults, used only when an individual item carries no aspect of its own (Image.aspect
  // is optional). "image" gets a wide single-shot default; the two grids match the S4 prototype's
  // own tile classes (.square, .poster).
  const defaultAspect = $derived(
    { image: '16 / 9', 'image-grid': '1 / 1', 'poster-grid': '2 / 3' }[block.type]
  );
  const defaultColumns = $derived({ image: 1, 'image-grid': 3, 'poster-grid': 5 }[block.type]);
  const columns = $derived(block.columns ?? defaultColumns);
</script>

<div class="media" class:single={block.type === 'image'} style="--cols: {columns}">
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#if block.items.length === 0}
    <p class="empty">{block.empty ?? 'No images yet'}</p>
  {:else}
    <div class="grid">
      {#each block.items as item, i (item.id ?? i)}
        {#if item.link}
          <a href={item.link} class="tile-link" target="_blank" rel="noreferrer">
            <MediaTile {item} {defaultAspect} />
          </a>
        {:else}
          <MediaTile {item} {defaultAspect} />
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  h4 {
    margin: 0 0 var(--v-s-2);
    font-size: 12px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .media {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .empty {
    margin: auto;
    color: var(--v-faint);
    font-size: 13px;
    text-align: center;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(var(--cols), 1fr);
    grid-auto-rows: 1fr;
    gap: var(--v-s-2);
    flex: 1;
    min-height: 0;
  }
  .single .grid {
    grid-template-columns: 1fr;
  }
  .tile-link {
    display: block;
    color: inherit;
    text-decoration: none;
    border-radius: var(--v-r-md);
  }
  .tile-link :global(.tile) {
    height: 100%;
  }
  @container (max-width: 420px) {
    .grid {
      grid-template-columns: repeat(3, 1fr);
    }
  }
</style>
