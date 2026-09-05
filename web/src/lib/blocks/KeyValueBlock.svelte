<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { formatValue } from '../format';

  // key-value is always the row layout (up to 24 items), regardless of count - unlike "metrics",
  // which only switches to rows below three items. A settings-style list of many rows is exactly
  // what this block is for.
  type Block = Extract<WidgetDocument['blocks'][number], { type: 'key-value' }>;
  let { block }: { block: Block } = $props();
</script>

<div class="kv">
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#each block.items as item, i (i)}
    <div class="row level-{item.level ?? 'unknown'}">
      <span class="label">{item.label}</span>
      <span class="value">{formatValue(item.value, item.format, item.unit)}</span>
    </div>
  {/each}
</div>

<style>
  .kv {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-2);
    height: 100%;
    justify-content: center;
  }
  h4 {
    margin: 0 0 var(--v-s-1);
    font-size: 12px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--v-s-3);
    padding-bottom: var(--v-s-2);
    border-bottom: 1px solid var(--v-border);
  }
  .row:last-child {
    border-bottom: 0;
    padding-bottom: 0;
  }
  .label {
    font-size: 13px;
    color: var(--v-muted);
  }
  .value {
    font: 500 15px/1 var(--v-font-num);
    font-variant-numeric: tabular-nums;
    color: var(--v-text);
  }
  .level-warn .value {
    color: var(--v-warn);
  }
  .level-error .value {
    color: var(--v-error);
  }
  .level-ok .value {
    color: var(--v-ok);
  }
</style>
