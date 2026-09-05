<script lang="ts">
  import type { Snippet } from 'svelte';
  import Status from './Status.svelte';
  import type { CardState, Span, StatusLevel } from './types';

  /**
   * The card shell: head, a body that fills its box, and an optional notice strip.
   *
   * The four execution states are rendered here rather than left to each block, because they are
   * core-owned facts about the run, not content (schemas/card-state.v1.schema.json). In
   * particular `stale` dims its content and says how old it is - it never blanks. A homelab
   * dashboard spends much of its life stale, and it has to look deliberate rather than broken.
   */
  interface Props {
    title: string;
    /** Short glyph or initials until the icon proxy lands (milestone L2). */
    icon?: string;
    href?: string;
    state?: CardState;
    /** Status shown in the head. Level defaults from the state. */
    statusLevel?: StatusLevel;
    statusText?: string;
    statusLabel?: string;
    /** Human age of the retained document, shown on the stale notice. */
    age?: string;
    /** Failure reason for `error`, and what happens next. */
    error?: string;
    retry?: string;
    /** Why the card will not run without a human (`disabled` only). */
    disabledReason?: string;
    span?: Span;
    children?: Snippet;
  }
  let {
    title,
    icon,
    href,
    state = 'ok',
    statusLevel,
    statusText,
    statusLabel,
    age,
    error,
    retry,
    disabledReason,
    span = {},
    children
  }: Props = $props();

  const level: StatusLevel = $derived(
    statusLevel ??
      (state === 'ok'
        ? 'ok'
        : state === 'stale'
          ? 'warn'
          : state === 'error'
            ? 'error'
            : state === 'disabled'
              ? 'muted'
              : 'unknown')
  );

  const text: string | undefined = $derived(
    statusText ??
      (state === 'stale'
        ? 'Stale'
        : state === 'error'
          ? 'Error'
          : state === 'pending'
            ? 'Loading'
            : state === 'disabled'
              ? 'Disabled'
              : undefined)
  );

  const label = $derived(statusLabel ?? text ?? 'Online');
  const style = $derived(
    `grid-column: span ${span.columns ?? 1}; grid-row: span ${span.rows ?? 1};`
  );
</script>

<article class="card" class:stale={state === 'stale'} {style}>
  <header>
    {#if icon}<span class="icon" aria-hidden="true">{icon}</span>{/if}
    {#if href}
      <h3><a {href} rel="noreferrer">{title}</a></h3>
    {:else}
      <h3>{title}</h3>
    {/if}
    <span class="spacer"></span>
    <Status {level} text={text} {label} />
  </header>

  {#if state === 'error'}
    <div class="body">
      <div class="fill centred"><p class="empty">No data collected yet</p></div>
      <p class="notice error">
        {error ?? 'Upstream request failed'}
        {#if retry}<span class="age">retry {retry}</span>{/if}
      </p>
    </div>
  {:else if state === 'disabled'}
    <div class="body">
      <div class="fill centred">
        <p class="empty">{disabledReason ?? 'Not running'}</p>
      </div>
    </div>
  {:else}
    <div class="body">
      <div class="fill" class:dimmed={state === 'stale'}>
        {@render children?.()}
      </div>
      {#if state === 'stale'}
        <p class="notice warn">
          Last refresh failed{#if age}<span class="age">{age}</span>{/if}
        </p>
      {/if}
    </div>
  {/if}
</article>

<style>
  .card {
    background: var(--v-surface);
    border: 1px solid var(--v-border);
    border-radius: var(--v-r-lg);
    box-shadow: var(--v-shadow);
    padding: var(--v-s-4);
    display: flex;
    flex-direction: column;
    gap: var(--v-s-3);
    container-type: inline-size;
    min-height: 0;
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    gap: var(--v-s-2);
  }
  .icon {
    width: 22px;
    height: 22px;
    border-radius: var(--v-r-sm);
    flex: none;
    background: var(--v-surface-2);
    display: grid;
    place-items: center;
    font-size: 11px;
    color: var(--v-muted);
  }
  h3 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  h3 a {
    color: inherit;
    text-decoration: none;
  }
  h3 a:hover {
    color: var(--v-accent);
  }
  .spacer {
    flex: 1;
  }

  /* The head and the notice keep their natural size; the body takes the slack, so content fills
   * the card rather than pooling at the top. */
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--v-s-3);
  }
  .fill {
    flex: 1;
    min-height: 0;
  }
  .centred {
    display: grid;
    place-content: center;
    text-align: center;
  }
  .dimmed {
    opacity: 0.55;
    filter: saturate(0.65);
  }

  .empty {
    margin: 0;
    color: var(--v-faint);
    font-size: 13px;
  }

  .notice {
    margin: 0;
    display: flex;
    align-items: center;
    gap: var(--v-s-2);
    font-size: 12px;
    padding: 7px 9px;
    border-radius: var(--v-r-md);
    border: 1px solid var(--v-border);
    background: var(--v-surface-2);
    color: var(--v-muted);
  }
  .notice.warn {
    border-color: color-mix(in oklab, var(--v-warn) 40%, var(--v-border));
    color: var(--v-warn);
  }
  .notice.error {
    border-color: color-mix(in oklab, var(--v-error) 40%, var(--v-border));
    color: var(--v-error);
  }
  .age {
    margin-left: auto;
    font: 400 11px/1 var(--v-font-num);
    color: var(--v-faint);
  }
</style>
