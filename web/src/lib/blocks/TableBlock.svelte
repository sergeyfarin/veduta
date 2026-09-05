<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import { formatValue } from '../format';

  type Block = Extract<WidgetDocument['blocks'][number], { type: 'table' }>;
  let { block }: { block: Block } = $props();
</script>

<div class="table-block">
  {#if block.title}<h4>{block.title}</h4>{/if}
  <div class="scroll">
    <table>
      <thead>
        <tr>
          {#each block.columns as col, i (i)}
            <th class="align-{col.align ?? 'start'}">{col.label ?? col.key}</th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each block.rows as row, i (i)}
          <tr>
            {#each block.columns as col, j (j)}
              <td class="align-{col.align ?? 'start'}">{formatValue(row[col.key], col.format)}</td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
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
  .table-block {
    height: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  /* Both axes: the schema allows up to 100 rows and 8 columns, either of which can exceed a
   * card's fixed height (B1's quantised row rhythm) or width. Wide content scrolls inside its
   * own container rather than the page ever scrolling horizontally. */
  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  th,
  td {
    padding: 6px 10px 6px 0;
    white-space: nowrap;
  }
  th {
    position: sticky;
    top: 0;
    background: var(--v-surface);
    text-align: left;
    font-size: 11px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border-bottom: 1px solid var(--v-border);
  }
  td {
    border-bottom: 1px solid var(--v-border);
    color: var(--v-text);
    font-variant-numeric: tabular-nums;
  }
  tbody tr:last-child td {
    border-bottom: 0;
  }
  .align-center {
    text-align: center;
  }
  .align-end {
    text-align: right;
  }
</style>
