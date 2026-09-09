<script lang="ts">
  import type { WidgetDocument } from '../types/widget';
  import Icon from '../Icon.svelte';

  // Rendered, real, and permanently disabled in 0.1 (docs/02-implementation-plan.md B4): the
  // buttons a card declares are visible, but there is no wiring behind them yet. Executing an
  // action needs authentication, an authorization check and an audit trail on the core side
  // (docs/01-architecture.md section 8), none of which exists before Phase D/H land - showing an
  // enabled button that does nothing on click would be worse than showing a disabled one.
  type Block = Extract<WidgetDocument['blocks'][number], { type: 'actions' }>;
  let { block }: { block: Block } = $props();
</script>

<div class="actions">
  {#if block.title}<h4>{block.title}</h4>{/if}
  <div class="row">
    {#each block.actions as action, i (i)}
      <button
        type="button"
        disabled
        class:danger={action.danger}
        title="Actions are not available yet"
      >
        {#if action.icon}<span class="icon" aria-hidden="true"><Icon spec={action.icon} /></span>{/if}
        {action.label}
      </button>
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
  .actions {
    display: flex;
    flex-direction: column;
    justify-content: center;
    height: 100%;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--v-s-2);
  }
  button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font: inherit;
    font-size: 13px;
    padding: 6px 12px;
    border-radius: var(--v-r-sm);
    border: 1px solid var(--v-border);
    background: var(--v-surface-2);
    color: var(--v-muted);
    cursor: not-allowed;
  }
  button.danger {
    border-color: color-mix(in oklab, var(--v-error) 35%, var(--v-border));
    color: color-mix(in oklab, var(--v-error) 60%, var(--v-muted));
  }
  .icon {
    width: 14px;
    height: 14px;
    display: grid;
    place-items: center;
    font-size: 12px;
  }
</style>
