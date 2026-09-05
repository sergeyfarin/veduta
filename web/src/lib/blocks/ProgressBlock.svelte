<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { formatValue } from '../format';

  type Block = Extract<WidgetDocument['blocks'][number], { type: 'progress' }>;
  let { block }: { block: Block } = $props();

  // progress is always 0-1 (schemas/widget-document.v1.schema.json: progressItem.progress,
  // minimum 0, maximum 1) - a schema-valid value outside that range should not exist, but a
  // hostile or buggy integration is exactly what clamping guards against here, in the renderer,
  // which is the last line of defence before a CSS width literally overflows its own bar.
  const clamp = (n: number) => Math.min(1, Math.max(0, n));
</script>

<div class="progress">
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#each block.items as item, i (i)}
    <div class="bar-row">
      <span class="label">{item.label}</span>
      {#if item.value !== undefined}
        <span class="val">{formatValue(item.value, item.format)}</span>
      {/if}
      <span
        class="bar"
        class:warn={item.level === 'warn'}
        class:error={item.level === 'error'}
        role="progressbar"
        aria-valuenow={Math.round(clamp(item.progress) * 100)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={item.label}
      >
        <span style="width: {clamp(item.progress) * 100}%"></span>
      </span>
    </div>
  {/each}
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
  .progress {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-3);
    justify-content: center;
    height: 100%;
  }
  .bar-row {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 4px var(--v-s-3);
    align-items: baseline;
  }
  .label {
    font-size: 13px;
    color: var(--v-text);
  }
  .val {
    font: 500 13px/1 var(--v-font-num);
    font-variant-numeric: tabular-nums;
    color: var(--v-muted);
  }
  .bar {
    grid-column: 1 / -1;
    display: block;
    height: 5px;
    border-radius: 999px;
    background: var(--v-surface-2);
    overflow: hidden;
  }
  .bar > span {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: var(--v-accent);
    transition: width 200ms ease;
  }
  .bar.warn > span {
    background: var(--v-warn);
  }
  .bar.error > span {
    background: var(--v-error);
  }
</style>
