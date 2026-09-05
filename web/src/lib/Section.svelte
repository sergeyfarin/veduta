<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title?: string;
    children: Snippet;
  }
  let { title, children }: Props = $props();
</script>

<section>
  {#if title}<h2>{title}</h2>{/if}
  {@render children()}
</section>

<style>
  section {
    margin-bottom: var(--v-s-6);
  }
  h2 {
    margin: 0 0 var(--v-s-3);
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--v-faint);
  }
</style>
