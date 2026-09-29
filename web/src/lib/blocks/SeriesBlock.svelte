<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import type { CardHistory } from '../types/cardstate';
  import { formatValue } from '../format';
  import { VIEW_HEIGHT, VIEW_WIDTH, extent, geometry, latest, resolveLines } from './series';

  type Block = Extract<WidgetDocument['blocks'][number], { type: 'series' }>;
  let { block, history }: { block: Block; history?: CardHistory } = $props();

  const lines = $derived(resolveLines(block, history));
  const g = $derived(geometry(block, history));
  const fmt = (v: number | undefined) => formatValue(v, block.format, block.unit);

  // Lines are told apart by dash pattern as well as colour, so the legend still works for a reader
  // who cannot see the difference between the two - the UI never carries meaning in colour alone.
  const DASHES = [undefined, '5 3', '1.5 2.5', '8 3 1.5 3'];
  const PALETTE = ['var(--v-accent)', 'var(--v-muted)', 'var(--v-faint)', 'var(--v-text)'];
  const LEVEL_COLOUR: Record<string, string> = {
    ok: 'var(--v-ok)',
    warn: 'var(--v-warn)',
    error: 'var(--v-error)'
  };
  const colour = (i: number) => LEVEL_COLOUR[lines[i]?.level ?? ''] ?? PALETTE[i];

  // The text alternative is the chart's content in words, not a description of a picture: what
  // each line reads now, and the range it moved through.
  const summary = $derived.by(() => {
    const parts = lines.map((line) => {
      const now = latest(line.points);
      const range = extent(line.points);
      if (now === undefined || !range) return `${line.label}: no readings`;
      return `${line.label}: ${fmt(now)} now, between ${fmt(range[0])} and ${fmt(range[1])}`;
    });
    const span = g.spanMs > 0 ? `over ${formatValue(g.spanMs / 1000, 'duration')}` : '';
    const lead = [block.title, span].filter(Boolean).join(' ');
    const heading = lead ? lead[0]!.toUpperCase() + lead.slice(1) + '. ' : '';
    return heading + parts.join('; ');
  });
</script>

<figure class="series">
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#if g.hasData}
    <svg
      viewBox="0 0 {VIEW_WIDTH} {VIEW_HEIGHT}"
      preserveAspectRatio="none"
      role="img"
      aria-label={summary}
    >
      <line class="baseline" x1="0" x2={VIEW_WIDTH} y1={VIEW_HEIGHT - 0.5} y2={VIEW_HEIGHT - 0.5} />
      {#each g.lines as line, i (i)}
        <g stroke={colour(i)} fill={colour(i)} stroke-dasharray={DASHES[i]}>
          {#each line.runs as points, j (j)}
            <polyline {points} fill="none" />
          {/each}
          {#each line.dots as [cx, cy], j (j)}
            <circle {cx} {cy} r="1.6" stroke="none" />
          {/each}
        </g>
      {/each}
    </svg>
    <div class="axis">
      <span>{fmt(g.low)}–{fmt(g.high)}</span>
      {#if g.spanMs > 0}<span>{formatValue(g.spanMs / 1000, 'duration')}</span>{/if}
    </div>
  {:else}
    <p class="empty">No readings yet</p>
  {/if}
  <figcaption>
    {#each lines as line, i (i)}
      <span class="entry">
        <svg class="swatch" viewBox="0 0 16 4" aria-hidden="true">
          <line x1="0" x2="16" y1="2" y2="2" stroke={colour(i)} stroke-dasharray={DASHES[i]} />
        </svg>
        <span class="label">{line.label}</span>
        <span class="value">{fmt(latest(line.points))}</span>
      </span>
    {/each}
  </figcaption>
</figure>

<style>
  figure {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: var(--v-s-2);
    height: 100%;
    justify-content: center;
  }
  h4 {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  svg[role='img'] {
    display: block;
    width: 100%;
    height: 64px;
    overflow: visible;
  }
  polyline,
  line {
    stroke-width: 1.75;
    stroke-linejoin: round;
    stroke-linecap: round;
    /* The viewBox is stretched to the card's width; without this the stroke would stretch too. */
    vector-effect: non-scaling-stroke;
  }
  .baseline {
    stroke: var(--v-border);
    stroke-width: 1;
  }
  .axis {
    display: flex;
    justify-content: space-between;
    font: 500 11px/1 var(--v-font-num);
    font-variant-numeric: tabular-nums;
    color: var(--v-faint);
  }
  figcaption {
    display: flex;
    flex-wrap: wrap;
    gap: var(--v-s-1) var(--v-s-4);
  }
  .entry {
    display: inline-flex;
    align-items: center;
    gap: var(--v-s-2);
    font-size: 13px;
  }
  .swatch {
    width: 16px;
    height: 4px;
  }
  .label {
    color: var(--v-muted);
  }
  .value {
    font: 500 13px/1 var(--v-font-num);
    font-variant-numeric: tabular-nums;
    color: var(--v-text);
  }
  .empty {
    margin: 0;
    font-size: 13px;
    color: var(--v-faint);
  }
</style>
