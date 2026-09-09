<script lang="ts">
  import { iconFallback, iconSource } from './icons';

  let { spec }: { spec: string } = $props();
  const source = $derived(iconSource(spec));
  const fallback = $derived(iconFallback(spec));
  let failed = $state(false);

  $effect(() => {
    source;
    failed = false;
  });
</script>

{#if source && !failed}
  <img class="image" src={source} alt="" onerror={() => (failed = true)} />
{:else}
  <span>{fallback}</span>
{/if}

<style>
  .image {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
</style>
