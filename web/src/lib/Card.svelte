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
  // Custom properties only - grid-column/grid-row are set in this component's own CSS, which
  // clamps them per breakpoint below. Setting the grid properties directly via inline style
  // would fight that clamp: an inline value always wins over a stylesheet rule of equal
  // specificity, which is exactly the bug this replaced (see the comment on .card below).
  const style = $derived(`--span-cols: ${span.columns ?? 1}; --span-rows: ${span.rows ?? 1};`);
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
    grid-column: span var(--span-cols, 1);
    grid-row: span var(--span-rows, 1);
  }
  /* Grid.svelte collapses its own explicit column count at these same breakpoints. A card
   * spanning more columns than the grid explicitly defines does not get clamped by the
   * browser - it gets an EXTRA implicit column invented to satisfy the span, which is exactly
   * as broken as it sounds: at a 380px width, a span-2 card produced a real 282px column next
   * to a 34px sliver holding half a neighbouring card, not a single full-width column. Each
   * breakpoint here must clamp the span to what Grid actually offers at that width. */
  @media (max-width: 1000px) {
    .card { grid-column: span min(var(--span-cols, 1), 2); }
  }
  @media (max-width: 620px) {
    .card { grid-column: span 1; }
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
  /* Stale content stays fully legible - it is retained BECAUSE it is still useful, and B3's
   * real block renderers made a real WCAG failure visible that B1's placeholder text never
   * could: --v-faint already sits at 4.71:1 on --v-surface, barely above the 4.5:1 body-text
   * floor, so ANY opacity reduction (even 0.9) pushes it below AA. Desaturation alone does the
   * job instead: near-grey text tokens are ~unaffected by saturate() (their contrast moves by
   * hundredths, confirmed numerically), while the accent/level colours a stale card actually
   * wants to look "not live" - the progress bar fill, a level-tinted value - genuinely mute
   * toward grey. The notice strip and status dot remain the primary, explicit staleness signal;
   * this is a secondary visual cue, not the only one, so it does not need to fight legibility to
   * do its job. */
  .dimmed {
    filter: saturate(0.4);
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
