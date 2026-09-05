<script lang="ts">
  import type { StatusLevel } from './types';

  /**
   * A status dot, and text only when the text says something the dot cannot.
   *
   * Spike S4: a green dot beside the word "Online" is two channels carrying the same nothing. The
   * dot is always present; `text` is for information the colour cannot express - an uptime, a
   * container count, a failure reason. Because abnormal states always carry text, the *presence*
   * of text is itself a non-colour signal, which is what keeps this accessible.
   */
  interface Props {
    level?: StatusLevel;
    /** Shown beside the dot. Omit for healthy-and-unremarkable. */
    text?: string;
    /** Always announced to assistive technology, and shown on hover, even when text is omitted. */
    label: string;
  }
  let { level = 'unknown', text, label }: Props = $props();
</script>

<span class="status {level}" class:bare={!text} title={text ? undefined : label}>
  <span class="dot" aria-hidden="true"></span>
  {#if text}{text}{:else}<span class="sr">{label}</span>{/if}
</span>

<style>
  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--v-muted);
    white-space: nowrap;
  }
  .status.bare {
    gap: 0;
    cursor: help;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--v-faint);
    flex: none;
  }
  .ok .dot { background: var(--v-ok); }
  .warn .dot { background: var(--v-warn); }
  .error .dot { background: var(--v-error); }
  .ok { color: var(--v-ok); }
  .warn { color: var(--v-warn); }
  .error { color: var(--v-error); }

  /* Visible to a screen reader, not to the eye. */
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
