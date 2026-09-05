<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import Status from '../Status.svelte';

  // Named "StatusListBlock" (not "StatusBlock") to keep it visually distinct in the codebase from
  // Status.svelte, which renders Document.status - a card's OWN overall health, a single value.
  // This block is a LIST of named statuses (e.g. one row per monitored host), reusing the same
  // Status.svelte component per row so the "dot always, text only when it says something" rule
  // (B1, spike S4) applies identically everywhere a status appears.
  type Block = Extract<WidgetDocument['blocks'][number], { type: 'status' }>;
  let { block }: { block: Block } = $props();

  // A word for Status's required accessible label, distinct from the row's own visible label -
  // otherwise a screen reader (and, as a proxy for that, a text query) would hear/find the same
  // string twice per row: once as the row label, once again as Status's hidden fallback.
  const levelWord: Record<string, string> = {
    ok: 'Healthy',
    warn: 'Warning',
    error: 'Error',
    info: 'Info',
    muted: 'Inactive',
    unknown: 'Unknown status'
  };
</script>

<div class="statuses">
  {#if block.title}<h4>{block.title}</h4>{/if}
  {#each block.items as item, i (i)}
    <div class="row">
      <span class="label">{item.label}</span>
      <Status level={item.level} text={item.text} label={item.text ?? levelWord[item.level] ?? 'Unknown status'} />
    </div>
  {/each}
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
  .statuses {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-2);
    justify-content: center;
    height: 100%;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--v-s-3);
    font-size: 13px;
  }
  .label {
    color: var(--v-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
