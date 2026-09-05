<script lang="ts">
  import Card from './lib/Card.svelte';
  import BlockRenderer from './lib/blocks/BlockRenderer.svelte';
  import Grid from './lib/Grid.svelte';
  import Section from './lib/Section.svelte';
  import Skeleton from './lib/Skeleton.svelte';
  import { apply, next, stored, type Theme } from './lib/theme';
  import type { BuildInfo } from './lib/types';

  /**
   * B1 supplied the shell (tokens, grid, card states, theme). B3 fills six of the nine block
   * types with real renderers - status, metrics, key-value, progress, list, text/markdown; the
   * remaining three (image, image-grid/poster-grid, table, actions) are B4 and later. The layout
   * itself still comes from configuration only in C1; until then this page is a fixed showcase
   * exercising every card state AND every B3 block type, reviewable against the S4 prototype.
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
    <span class="sub">B3 — block renderers: status, metrics, key-value, progress, list, text</span>
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
        <BlockRenderer
          block={{
            type: 'progress',
            items: [
              { label: 'CPU', progress: 0.12 },
              { label: 'Memory', progress: 0.61 },
              { label: 'Root', progress: 0.92, level: 'error' }
            ]
          }}
        />
      </Card>

      <Card title="Proxmox" icon="PX" state="stale" age="7m ago" span={{ rows: 2 }}>
        <BlockRenderer
          block={{
            type: 'metrics',
            items: [
              { label: 'VMs', value: 7, format: 'count' },
              { label: 'CPU', value: 0.23, format: 'percent' },
              { label: 'Uptime', value: 8294400, format: 'duration' }
            ]
          }}
        />
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
        <BlockRenderer
          block={{
            type: 'key-value',
            items: [
              { label: 'Queries today', value: 184902, format: 'count' },
              { label: 'Blocked', value: 0.314, format: 'percent' }
            ]
          }}
        />
      </Card>

      <Card title="Docker" icon="DK" statusText="14 running" span={{ columns: 2, rows: 2 }}>
        <BlockRenderer
          block={{
            type: 'list',
            items: [
              { title: 'immich-server', value: '2.1 GB' },
              { title: 'jellyfin', value: '1.4 GB' },
              { title: 'frigate', level: 'warn', value: 'restarting' },
              { title: 'adguard-home', value: '86 MB' }
            ]
          }}
        />
      </Card>

      <Card title="Hosts" icon="HS" span={{ rows: 2 }}>
        <BlockRenderer
          block={{
            type: 'status',
            items: [
              { label: 'coding-server', level: 'ok' },
              { label: 'nas', level: 'ok' },
              { label: 'router', level: 'warn', text: 'high latency' }
            ]
          }}
        />
      </Card>

      <Card title="Notes" icon="NT" span={{ columns: 2, rows: 2 }}>
        <BlockRenderer
          block={{
            type: 'markdown',
            content:
              '**Maintenance window** this weekend for the *Proxmox* host - see the [runbook](https://example.com/runbook) beforehand.\n\n- Snapshot every VM first\n- Confirm backups landed on the NAS'
          }}
        />
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
