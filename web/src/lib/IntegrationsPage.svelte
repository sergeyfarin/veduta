<script lang="ts">
  import Status from './Status.svelte';
  import { loadIntegrations, routeText, type IntegrationView } from './integrations';

  /**
   * Phase M4: a read-only view of every integration's authority. It shows what each one is granted
   * and what it asks for beyond that, and says which command approves it - it never approves
   * anything itself. Approval stays at the CLI, where the operator is at a shell on the host and
   * the security model is most defensible (docs/03-backlog-resolved.md, "The approval API had no
   * client"). A load callback is injectable so the component can be tested without a server.
   */
  let { load = loadIntegrations }: { load?: () => Promise<IntegrationView[]> } = $props();

  let views = $state<IntegrationView[] | null>(null);
  let error = $state<string | null>(null);

  $effect(() => {
    load()
      .then((v) => (views = v))
      .catch((err: unknown) => (error = err instanceof Error ? err.message : String(err)));
  });
</script>

<section class="integrations" aria-labelledby="integrations-heading">
  <h2 id="integrations-heading">Integrations</h2>
  <p class="lede">
    What each integration may reach, and what it asks for beyond that. This page changes nothing:
    approving is done at the command line on the Veduta host.
  </p>

  {#if error}
    <p class="error-banner" role="alert">Could not load integrations: {error}</p>
  {:else if views === null}
    <p class="muted">Loading…</p>
  {:else if views.length === 0}
    <p class="muted">No integrations are declared in the configuration.</p>
  {:else}
    {#each views as v (v.id)}
      <article class="integration" data-integration={v.id}>
        <header>
          <h3>{v.title}</h3>
          <Status level={v.level} label={v.statusLabel} text={v.statusLabel} />
        </header>
        <p class="meaning">{v.meaning}</p>
        {#if v.error}<p class="error-text">{v.error}</p>{/if}

        {#if v.granted.length}
          <h4>Granted</h4>
          <ul class="routes">
            {#each v.granted as r, i (i)}
              <li><code>{routeText(r)}</code>{#if r.reason}<span class="reason">{r.reason}</span>{/if}</li>
            {/each}
          </ul>
          {#if v.capabilities.length}
            <p class="caps">Capabilities: {v.capabilities.join(', ')}</p>
          {/if}
        {/if}

        {#if v.notGranted.length}
          <h4>Requested, not approved</h4>
          <ul class="routes pending" data-kind="not-granted">
            {#each v.notGranted as r, i (i)}
              <li><code>{routeText(r)}</code>{#if r.reason}<span class="reason">{r.reason}</span>{/if}</li>
            {/each}
          </ul>
        {/if}
        {#if v.newCapabilities.length}
          <p class="caps pending">New capabilities requested: {v.newCapabilities.join(', ')}</p>
        {/if}
        {#if v.raisedLimits.length}
          <h4>Limits raised above the approval</h4>
          <ul class="limits pending">
            {#each v.raisedLimits as l (l.field)}
              <li><code>{l.field}</code> {l.current} → {l.requested}</li>
            {/each}
          </ul>
        {/if}
        {#if v.noLongerRequested.length}
          <h4>Approved, no longer requested</h4>
          <ul class="routes" data-kind="no-longer-requested">
            {#each v.noLongerRequested as r, i (i)}
              <li><code>{routeText(r)}</code></li>
            {/each}
          </ul>
        {/if}
        {#if v.bodyBearing.length}
          <p class="body-note">
            Sends a request body on {v.bodyBearing.map((r) => `${r.method} ${r.path}`).join(', ')}.
            A route grant cannot see inside a body, so approving one approves whatever that body
            can ask the service to do.
          </p>
        {/if}

        {#if v.commands}
          <h4>To review and approve</h4>
          <pre class="commands">{v.commands.join('\n')}</pre>
        {/if}

        {#if v.requestedLimits.length}
          <details>
            <summary>Limits requested</summary>
            <dl class="limit-grid">
              {#each v.requestedLimits as [name, value] (name)}
                <dt>{name}</dt>
                <dd>{value}</dd>
              {/each}
            </dl>
          </details>
        {/if}
        {#if v.limits.length}
          <details>
            <summary>Effective limits</summary>
            <dl class="limit-grid">
              {#each v.limits as [name, value] (name)}
                <dt>{name}</dt>
                <dd>{value}</dd>
              {/each}
            </dl>
          </details>
        {/if}
      </article>
    {/each}
  {/if}
</section>

<style>
  .integrations {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-4);
  }
  h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
  }
  .lede,
  .meaning {
    margin: 0;
    font-size: 13px;
    color: var(--v-muted);
  }
  .integration {
    display: flex;
    flex-direction: column;
    gap: var(--v-s-2);
    padding: var(--v-s-4);
    border: 1px solid var(--v-border);
    border-radius: var(--v-r-md);
    background: var(--v-surface);
  }
  .integration header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--v-s-3);
  }
  h3 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  h4 {
    margin: var(--v-s-2) 0 0;
    font-size: 11px;
    font-weight: 600;
    color: var(--v-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  li {
    display: flex;
    flex-wrap: wrap;
    gap: 4px var(--v-s-3);
    align-items: baseline;
    font-size: 13px;
  }
  code,
  pre {
    font: 12px/1.5 var(--v-font-num);
    color: var(--v-text);
  }
  .pending code,
  .pending {
    color: var(--v-warn);
  }
  .reason {
    color: var(--v-muted);
    font-size: 12px;
  }
  .caps,
  .body-note {
    margin: 0;
    font-size: 12px;
    color: var(--v-muted);
  }
  .commands {
    margin: 0;
    padding: var(--v-s-3);
    border-radius: var(--v-r-sm);
    background: var(--v-surface-2);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  details {
    font-size: 12px;
    color: var(--v-muted);
  }
  .limit-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 2px var(--v-s-4);
    margin: var(--v-s-2) 0 0;
  }
  .limit-grid dt {
    font-family: var(--v-font-num);
  }
  .limit-grid dd {
    margin: 0 0 var(--v-s-1);
    color: var(--v-text);
  }
  .error-text {
    margin: 0;
    font-size: 12px;
    color: var(--v-error);
    overflow-wrap: anywhere;
  }
  .error-banner {
    margin: 0;
    padding: var(--v-s-3) var(--v-s-4);
    border: 1px solid var(--v-border);
    border-radius: var(--v-r-sm);
    background: var(--v-surface-2);
    color: var(--v-muted);
    font-size: 13px;
  }
  .muted {
    margin: 0;
    color: var(--v-faint);
    font-size: 13px;
  }
</style>
