<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { formatValue } from '../format';
  import Icon from '../Icon.svelte';

  // Empty items is a legal, deliberate state (schemas/widget-document.v1.schema.json's own
  // description: "No active streams" is a state, not a bug) - rendered as `empty`, never as a
  // blank box that looks broken.
  type Block = Extract<WidgetDocument['blocks'][number], { type: 'list' }>;
  let { block }: { block: Block } = $props();
</script>

<div class="list">
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#if block.items.length === 0}
    <p class="empty">{block.empty ?? 'Nothing to show'}</p>
  {:else}
    {#each block.items as item, i (i)}
      <div class="row level-{item.level ?? 'unknown'}">
        {#if item.icon}<span class="icon" aria-hidden="true"><Icon spec={item.icon} /></span>{/if}
        <span class="text">
          <span class="name">{item.title}</span>
          {#if item.subtitle}<span class="subtitle">{item.subtitle}</span>{/if}
        </span>
        {#if item.value !== undefined}
          <span class="meta">{formatValue(item.value, item.format)}</span>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  h4 {
    margin: 0 0 var(--v-s-1);
    font-size: 12px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .list {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .empty {
    margin: auto;
    color: var(--v-faint);
    font-size: 13px;
    text-align: center;
  }
  .row {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: var(--v-s-2);
    padding: 6px 0;
    border-bottom: 1px solid var(--v-border);
    font-size: 13px;
  }
  .row:last-child {
    border-bottom: 0;
  }
  .icon {
    width: 18px;
    height: 18px;
    display: grid;
    place-items: center;
    font-size: 11px;
    color: var(--v-muted);
    flex: none;
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--v-text);
  }
  .subtitle {
    font-size: 11px;
    color: var(--v-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font: 400 12px/1 var(--v-font-num);
    color: var(--v-muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .level-warn .meta {
    color: var(--v-warn);
  }
  .level-error .meta {
    color: var(--v-error);
  }
</style>
