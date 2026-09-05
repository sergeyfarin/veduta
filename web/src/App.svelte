<script lang="ts">
  import Card from './lib/Card.svelte';
  import Grid from './lib/Grid.svelte';
  import Section from './lib/Section.svelte';
  import Skeleton from './lib/Skeleton.svelte';
  import { apply, next, stored, type Theme } from './lib/theme';
  import type { BuildInfo } from './lib/types';

  /**
   * Milestone B1: the shell - tokens, grid, card states, theme. The block renderers that fill a
   * card arrive in B3 and B4, and the layout comes from configuration in C1; until then this page
   * exercises every card state so the chrome can be reviewed against the S4 prototype.
   */
  let theme = $state<Theme>(stored());
  let build = $state<BuildInfo | null>(null);

  $effect(() => {
    apply(theme);
  });

  $effect(() => {
    fetch('/api/v1/version')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((info: BuildInfo) => (build = info))
      .catch(() => (build = null));
  });
</script>

<div class="page">
  <header class="topbar">
    <h1>Home</h1>
    <span class="sub">B1 — card shell and design tokens</span>
    <span class="spacer"></span>
    <button onclick={() => (theme = next(theme))}>
      Theme: {theme}
    </button>
  </header>

  <Section title="Card states">
    <Grid>
      <Card title="Jellyfin" icon="JF" span={{ columns: 2, rows: 2 }} href="#">
        <p class="placeholder">poster-grid — milestone B4</p>
      </Card>

      <Card title="coding-server" icon="CS" statusText="34d" span={{ rows: 2 }}>
        <p class="placeholder">progress — milestone B3</p>
      </Card>

      <Card title="Proxmox" icon="PX" state="stale" age="7m ago" span={{ rows: 2 }}>
        <p class="placeholder">metrics — retained, dimmed, never blanked</p>
      </Card>

      <Card
        title="Frigate"
        icon="FR"
        state="error"
        error="502 Bad Gateway"
        retry="40s"
        span={{ rows: 2 }}
      />

      <Card title="Uptime Kuma" icon="UP" state="pending" span={{ rows: 2 }}>
        <div class="pending">
          <Skeleton width="56px" />
          <Skeleton width="80px" />
        </div>
      </Card>

      <Card
        title="Immich"
        icon="IM"
        state="disabled"
        disabledReason="Awaiting approval"
        span={{ rows: 2 }}
      />

      <Card title="AdGuard" icon="AG" span={{ rows: 2 }}>
        <p class="placeholder">key-value pair — milestone B3</p>
      </Card>
    </Grid>
  </Section>

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
  .sub {
    color: var(--v-muted);
    font-size: 13px;
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

  .placeholder {
    margin: 0;
    color: var(--v-faint);
    font-size: 13px;
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
