<script lang="ts">
  import type { Snippet } from 'svelte';

  /**
   * The dashboard grid.
   *
   * Heights are quantised to --v-row rather than left to content, so cards of different content
   * still line up (spike S4). Homepage looks tidy because every tile is identical; that uniformity
   * is not available to a dashboard whose point is that a photo wall and a three-number card are
   * both first-class. A shared rhythm is.
   */
  interface Props {
    columns?: number;
    children: Snippet;
  }
  let { columns = 4, children }: Props = $props();
</script>

<div class="grid" style="--cols: {columns}">
  {@render children()}
</div>

<style>
  .grid {
    display: grid;
    gap: var(--v-s-4);
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    grid-auto-rows: var(--v-row);
    grid-auto-flow: dense;
  }
  /* Cards keep their row span at every width - the rhythm is the point - but stop spanning more
   * columns than exist. */
  @media (max-width: 1000px) {
    .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-width: 620px) {
    .grid { grid-template-columns: 1fr; }
  }
</style>
