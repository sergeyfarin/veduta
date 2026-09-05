<script lang="ts">
  import type { BuildInfo } from './lib/types';

  // Milestone A2 skeleton: proves the Go binary serves this build and the API is reachable.
  // The dashboard, the design system and the trusted Widget Document renderer arrive in Phase B.
  let build = $state<BuildInfo | null>(null);
  let error = $state<string | null>(null);

  $effect(() => {
    fetch('/api/v1/version')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(`HTTP ${r.status}`))))
      .then((info: BuildInfo) => (build = info))
      .catch((e: unknown) => (error = e instanceof Error ? e.message : String(e)));
  });
</script>

<main>
  <h1>Veduta</h1>
  <p class="tagline">A wide, detailed view of your homelab.</p>

  {#if build}
    <p class="build">
      {build.version} · <a href={build.sourceUrl}>source</a>
    </p>
  {:else if error}
    <p class="build muted">API not reachable yet ({error}) — expected until milestone A3.</p>
  {:else}
    <p class="build muted">Loading…</p>
  {/if}
</main>

<style>
  main {
    max-width: 40rem;
    margin: 4rem auto;
    padding: 0 1.5rem;
    color: var(--v-text);
  }
  h1 {
    margin: 0;
    font-size: 2rem;
    letter-spacing: -0.02em;
  }
  .tagline {
    color: var(--v-muted);
    margin: 0.25rem 0 2rem;
  }
  .build {
    font-size: 0.875rem;
  }
  .muted {
    color: var(--v-muted);
  }
</style>
