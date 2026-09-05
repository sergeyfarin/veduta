# web

The dashboard SPA: Svelte 5 (runes) + Vite + TypeScript, built to `build/app/` and embedded in the Go
binary. Deliberately **not** SvelteKit — Go serves and embeds everything, so a router, prerenderer
and adapter layer would buy nothing (docs/01-architecture.md §13).

Run it from the repository root, not here:

```
pnpm dev            # Go API on 127.0.0.1:8099 and Vite on :5173, /api proxied
pnpm dev -- --host  # same, but Vite also binds 0.0.0.0 - reach it from your phone/another device
pnpm build          # SPA into web/build/app, then the Go binary
pnpm check          # go vet, go test, svelte-check
```

## Pinned versions

Every dependency is an exact version (`.npmrc` sets `save-exact`), so an update is a reviewable
commit rather than something a fresh install decides.

**TypeScript is held at 5.9.3.** TypeScript 7 (the native port) is released, but `svelte-check`
4.7.6 fails to load it — it requires the CJS compiler API that the port does not expose. Revisit
when svelte-check supports it; the pin is the only thing standing between us and it.

## Reaching the dev server from another device

`pnpm dev -- --host` exposes only Vite (`0.0.0.0:5173`); the Go API keeps listening on
`127.0.0.1:8099` and is never exposed. That's safe and sufficient: Vite's dev-server process
proxies `/api/*` requests to `127.0.0.1:8099` itself, on the same host, so a remote browser only
ever talks to Vite - it never needs a route to the Go server directly. This does not conflict with
the server's own loopback-only bind refusal (`docs/01-architecture.md` D46), which is about the Go
process's own listener, not Vite's.

The `--` matters: `pnpm dev --host` (no `--`) does not work, because `concurrently`'s two child
commands are quoted strings and `--host` would be appended after them as an argument to
`concurrently` itself, not forwarded into the `vite` command. `pnpm dev -- --host` puts `--host`
after `concurrently -P ... --`, which is what makes `{@}` in the `web` script actually pick it up.
