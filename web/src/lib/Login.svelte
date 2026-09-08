<script lang="ts">
  let { onAuthenticated }: { onAuthenticated: () => void } = $props();
  let username = $state('');
  let password = $state('');
  let error = $state<string | null>(null);
  let submitting = $state(false);

  async function login(event: SubmitEvent) {
    event.preventDefault();
    submitting = true;
    error = null;
    try {
      const response = await fetch('/api/v1/auth/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password })
      });
      if (!response.ok) {
        const body = await response.json().catch(() => ({})) as { error?: string };
        throw new Error(body.error ?? `Login failed: HTTP ${response.status}`);
      }
      password = '';
      onAuthenticated();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      submitting = false;
    }
  }
</script>

<main class="login-page">
  <form class="login-card" onsubmit={login}>
    <div>
      <p class="eyebrow">Veduta</p>
      <h1>Sign in</h1>
      <p class="hint">Use the administrator account configured for this dashboard.</p>
    </div>
    <label>
      Username
      <input name="username" autocomplete="username" bind:value={username} required />
    </label>
    <label>
      Password
      <input name="password" type="password" autocomplete="current-password" bind:value={password} required />
    </label>
    {#if error}<p class="login-error" role="alert">{error}</p>{/if}
    <button type="submit" disabled={submitting}>{submitting ? 'Signing in…' : 'Sign in'}</button>
  </form>
</main>

<style>
  .login-page {
    min-height: 100vh;
    display: grid;
    place-items: center;
    padding: var(--v-s-5);
  }
  .login-card {
    width: min(100%, 360px);
    display: grid;
    gap: var(--v-s-5);
    padding: var(--v-s-6);
    border: 1px solid var(--v-border);
    border-radius: var(--v-r-lg);
    background: var(--v-surface);
    box-shadow: var(--v-shadow);
  }
  .eyebrow, h1, .hint, .login-error { margin: 0; }
  .eyebrow { color: var(--v-accent); font-size: 12px; font-weight: 650; letter-spacing: .08em; text-transform: uppercase; }
  h1 { margin-top: var(--v-s-2); font-size: 24px; }
  .hint { margin-top: var(--v-s-2); color: var(--v-muted); font-size: 13px; line-height: 1.5; }
  label { display: grid; gap: var(--v-s-2); color: var(--v-muted); font-size: 12px; font-weight: 600; }
  input {
    min-width: 0;
    border: 1px solid var(--v-border);
    border-radius: var(--v-r-sm);
    padding: 10px 12px;
    background: var(--v-surface-2);
    color: var(--v-text);
    font: inherit;
    font-size: 14px;
  }
  input:focus { outline: 2px solid var(--v-accent); outline-offset: 2px; }
  button {
    border: 0;
    border-radius: var(--v-r-sm);
    padding: 10px 12px;
    background: var(--v-accent);
    color: var(--v-accent-contrast, white);
    font: inherit;
    font-weight: 650;
    cursor: pointer;
  }
  button:disabled { cursor: wait; opacity: .65; }
  .login-error { color: var(--v-error); font-size: 13px; }
</style>
