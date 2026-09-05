<script lang="ts">
  import type { MediaItem } from '../types/widget';
  import { assetUrl, cssAspectRatio } from '../assets';
  import Skeleton from '../Skeleton.svelte';

  interface Props {
    item: MediaItem;
    /** Per-kind fallback used only when the item carries no aspect of its own. */
    defaultAspect: string;
  }
  let { item, defaultAspect }: Props = $props();

  let loaded = $state(false);
  let broken = $state(false);

  // Blur-up placeholder: a shimmer skeleton shown until the real image has decoded, then a plain
  // cross-fade. Image.blurhash exists in the schema for a true decoded-thumbnail placeholder;
  // implementing the BlurHash algorithm itself (decode -> pixel grid -> canvas) is real,
  // separable work deferred rather than rushed in alongside three new block types - this is a
  // deliberate, named scope boundary, not an oversight. See docs/02-implementation-plan.md B4.
  const aspect = $derived(cssAspectRatio(item.image.aspect) ?? defaultAspect);
</script>

<figure class="tile" style="aspect-ratio: {aspect}">
  {#if !loaded && !broken}
    <div class="placeholder"><Skeleton height="100%" width="100%" /></div>
  {/if}
  {#if broken}
    <div class="broken" role="img" aria-label={item.image.alt ?? item.title ?? 'Image failed to load'}>
      <span aria-hidden="true">🖼</span>
    </div>
  {:else}
    <img
      src={assetUrl(item.image.ref)}
      alt={item.image.alt ?? item.title ?? ''}
      loading="lazy"
      decoding="async"
      class:loaded
      onload={() => (loaded = true)}
      onerror={() => (broken = true)}
    />
  {/if}
  {#if item.title || item.subtitle}
    <figcaption class="cap">
      {#if item.title}<span class="title">{item.title}</span>{/if}
      {#if item.subtitle}<span class="subtitle">{item.subtitle}</span>{/if}
    </figcaption>
  {/if}
</figure>

<style>
  .tile {
    position: relative;
    margin: 0;
    border-radius: var(--v-r-md);
    overflow: hidden;
    background: var(--v-surface-2);
  }
  .placeholder {
    position: absolute;
    inset: 0;
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    opacity: 0;
    transition: opacity 200ms ease;
  }
  img.loaded {
    opacity: 1;
  }
  .broken {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    color: var(--v-faint);
    font-size: 22px;
  }
  .cap {
    position: absolute;
    inset: auto 0 0 0;
    margin: 0;
    padding: 14px 8px 6px;
    display: flex;
    flex-direction: column;
    font-size: 11px;
    color: var(--v-overlay-text);
    background: linear-gradient(to top, var(--v-overlay-scrim), transparent);
    opacity: 0;
    transition: opacity 120ms ease;
  }
  .tile:hover .cap,
  .tile:focus-within .cap {
    opacity: 1;
  }
  .title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .subtitle {
    opacity: 0.8;
  }
</style>
