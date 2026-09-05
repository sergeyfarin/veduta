<script lang="ts">
  import Card from './lib/Card.svelte';
  import BlockRenderer from './lib/blocks/BlockRenderer.svelte';
  import Grid from './lib/Grid.svelte';
  import Section from './lib/Section.svelte';
  import Skeleton from './lib/Skeleton.svelte';
  import { apply, next, stored, type Theme } from './lib/theme';
  import { formatRelativeTime } from './lib/format';
  import { isStale, isError, isDisabled, isPending, type CardState as CardEnvelope } from './lib/types/cardstate';
  import type { Dashboard } from './lib/types/dashboard';
  import type { BuildInfo, CardState as CardVisualState } from './lib/types';

  /**
   * The real render path (milestone B5): GET /dashboard supplies layout (sections, card
   * descriptors, spans), GET /cards supplies every card's current CardState envelope. Neither
   * endpoint exists outside dev today - only the --fixtures flag serves them, from the checked-in
   * showcase - but this component itself is the same one production configuration (Phase C) and
   * the scheduler (Phase F) will drive once they exist; only the data source changes underneath.
   */
  let theme = $state<Theme>(stored());
  let build = $state<BuildInfo | null>(null);
  let dashboard = $state<Dashboard | null>(null);
  let cardsById = $state<Map<string, CardEnvelope>>(new Map());
  let loadError = $state<string | null>(null);

  $effect(() => {
    apply(theme);
  });

  $effect(() => {
    fetch('/api/v1/version')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((info: BuildInfo) => (build = info))
      .catch(() => (build = null));
  });

  $effect(() => {
    Promise.all([
      fetch('/api/v1/dashboard').then((r) =>
        r.ok ? (r.json() as Promise<Dashboard>) : Promise.reject(new Error(`GET /dashboard: HTTP ${r.status}`))
      ),
      fetch('/api/v1/cards').then((r) =>
        r.ok ? (r.json() as Promise<CardEnvelope[]>) : Promise.reject(new Error(`GET /cards: HTTP ${r.status}`))
      )
    ])
      .then(([d, cards]) => {
        dashboard = d;
        cardsById = new Map(cards.map((c) => [c.cardId, c]));
        loadError = null;
      })
      .catch((err: unknown) => {
        loadError = err instanceof Error ? err.message : String(err);
      });
  });

  // disabledReason is core-owned and machine-readable (schemas/card-state.v1); this is the one
  // place it becomes the sentence a person reads on the card.
  const disabledReasonText: Record<string, string> = {
    unapproved: 'Awaiting approval',
    'permissions-changed': 'Permissions changed',
    'integration-missing': 'Integration missing',
    'config-error': 'Configuration error',
    operator: 'Turned off'
  };

  function visualState(cs: CardEnvelope | undefined): CardVisualState {
    return cs?.execution.state ?? 'pending';
  }
</script>

<div class="page">
  <header class="topbar">
    <h1>Home</h1>
    <span class="spacer"></span>
    <button onclick={() => (theme = next(theme))}>
      Theme: {theme}
    </button>
  </header>

  {#if loadError}
    <p class="error-banner">Could not load the dashboard: {loadError}</p>
  {/if}

  {#if dashboard}
    {#each dashboard.sections as section, i (section.title ?? i)}
      <Section title={section.title}>
        <Grid>
          {#each section.cards as descriptor (descriptor.id)}
            {@const cs = cardsById.get(descriptor.id)}
            <Card
              title={descriptor.title}
              icon={descriptor.icon}
              href={descriptor.href}
              span={descriptor.span}
              state={visualState(cs)}
              statusLevel={cs?.document?.status?.level}
              statusText={cs?.document?.status?.text}
              age={cs && isStale(cs) ? formatRelativeTime(cs.execution.staleSince) : undefined}
              error={cs && isError(cs) ? cs.execution.error?.message : undefined}
              retry={cs && isError(cs) && cs.execution.nextRunAt
                ? formatRelativeTime(cs.execution.nextRunAt)
                : undefined}
              disabledReason={cs && isDisabled(cs)
                ? (disabledReasonText[cs.execution.disabledReason] ?? cs.execution.disabledReason)
                : undefined}
            >
              {#if cs?.document?.blocks?.length}
                <div class="blocks">
                  {#each cs.document.blocks as block, i (i)}
                    <BlockRenderer {block} />
                  {/each}
                </div>
              {:else if cs && isPending(cs)}
                <div class="pending">
                  <Skeleton width="56px" />
                  <Skeleton width="80px" />
                </div>
              {/if}
            </Card>
          {/each}
        </Grid>
      </Section>
    {/each}
  {/if}

  <footer>
    {#if build}
      veduta {build.version} · <a href={build.sourceUrl}>source</a>
    {:else}
      <span class="muted">API not reachable</span>
    {/if}
  </footer>
</div>

<style>
  .page {
    max-width: 1280px;
    margin: 0 auto;
    padding: var(--v-s-6) var(--v-s-5) 64px;
  }
  .topbar {
    display: flex;
    align-items: baseline;
    gap: var(--v-s-4);
    margin-bottom: var(--v-s-6);
  }
  .topbar h1 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.02em;
  }
  .spacer {
    flex: 1;
  }
  button {
    border: 1px solid var(--v-border);
    background: var(--v-surface);
    color: var(--v-muted);
    border-radius: var(--v-r-sm);
    padding: 6px 10px;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }
  button:hover {
    color: var(--v-text);
  }

  .error-banner {
    margin: 0 0 var(--v-s-5);
    padding: var(--v-s-3) var(--v-s-4);
    border: 1px solid var(--v-border);
    border-radius: var(--v-r-sm);
    background: var(--v-surface-2);
    color: var(--v-muted);
    font-size: 13px;
  }

  .blocks {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-3);
    height: 100%;
    min-height: 0;
  }

  .pending {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-2);
    justify-content: center;
    height: 100%;
  }

  footer {
    margin-top: 48px;
    padding-top: var(--v-s-4);
    border-top: 1px solid var(--v-border);
    color: var(--v-faint);
    font-size: 12px;
  }
  footer a {
    color: var(--v-muted);
  }
  .muted {
    color: var(--v-faint);
  }
</style>
