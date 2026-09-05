<script lang="ts">
  // A block type this build does not recognise - most likely an older binary encountering a
  // newer integration's output, or vice versa. Rendered as a plainly-labelled placeholder rather
  // than crashing the whole card or silently vanishing, per docs/01-architecture.md section 13
  // ("unknown types render a labelled placeholder (forward compatibility)").
  let { type }: { type: string } = $props();
</script>

<div class="unknown">
  <span class="icon" aria-hidden="true">?</span>
  <span>Unsupported block: <code>{type}</code></span>
</div>

<style>
  .unknown {
    display: flex;
    align-items: center;
    gap: var(--v-s-2);
    height: 100%;
    justify-content: center;
    color: var(--v-faint);
    font-size: 12px;
    border: 1px dashed var(--v-border);
    border-radius: var(--v-r-md);
    padding: var(--v-s-3);
  }
  .icon {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: var(--v-surface-2);
    display: grid;
    place-items: center;
    flex: none;
  }
  code {
    font-family: var(--v-font-num);
  }
</style>
