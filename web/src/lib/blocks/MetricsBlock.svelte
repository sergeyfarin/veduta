<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { formatValue } from '../format';

  type Block = Extract<WidgetDocument['blocks'][number], { type: 'metrics' }>;
  let { block }: { block: Block } = $props();

  // S4 review: two figures in an equal-column grid each left-align into their half, leaving a
  // trailing void - it reads as unbalanced. One or two items therefore render as key-value ROWS
  // (value hard right), matching KeyValueBlock's own layout; three or more use columns, where the
  // width is genuinely used. This is a rendering rule, not a second block type - the schema still
  // has one "metrics" block.
  const pair = $derived(block.items.length <= 2);
</script>

<div class="metrics" class:pair style={pair ? undefined : `--cols: ${block.columns ?? 'auto'}`}>
  {#if block.title}<h4>{block.title}</h4>{/if}
  <div class="items" class:pair-items={pair}>
    {#each block.items as item, i (i)}
      <div class="metric level-{item.level ?? 'unknown'}">
        <span class="label">{item.label}</span>
        <span class="value">{formatValue(item.value, item.format, item.unit)}</span>
      </div>
    {/each}
  </div>
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
  .items {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(84px, 1fr));
    gap: var(--v-s-3);
  }
  .items.pair-items {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-2);
    justify-content: center;
    height: 100%;
  }
  .metric {
    display: flex;
    flex-direction: column;
  }
  .pair-items .metric {
    flex-direction: row;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--v-s-3);
    padding-bottom: var(--v-s-2);
    border-bottom: 1px solid var(--v-border);
  }
  .pair-items .metric:last-child {
    border-bottom: 0;
    padding-bottom: 0;
  }
  .label {
    font-size: 11px;
    color: var(--v-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .pair-items .label {
    text-transform: none;
    letter-spacing: 0;
    font-size: 13px;
  }
  .value {
    font: 500 20px/1.2 var(--v-font-num);
    font-variant-numeric: tabular-nums;
    margin-top: 2px;
    color: var(--v-text);
  }
  .pair-items .value {
    margin-top: 0;
    font-size: 18px;
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
