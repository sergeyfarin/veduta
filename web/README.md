# web

The dashboard SPA: Svelte 5 (runes) + Vite + TypeScript, built to `build/` and embedded in the Go
binary. Deliberately **not** SvelteKit — Go serves and embeds everything, so a router, prerenderer
and adapter layer would buy nothing (docs/01-architecture.md §13).

Run it from the repository root, not here:

```
pnpm dev      # Go API on 127.0.0.1:8099 and Vite on :5173, /api proxied
pnpm build    # SPA into web/build, then the Go binary
pnpm check    # go vet, go test, svelte-check
```

## Pinned versions

Every dependency is an exact version (`.npmrc` sets `save-exact`), so an update is a reviewable
commit rather than something a fresh install decides.

**TypeScript is held at 5.9.3.** TypeScript 7 (the native port) is released, but `svelte-check`
4.7.6 fails to load it — it requires the CJS compiler API that the port does not expose. Revisit
when svelte-check supports it; the pin is the only thing standing between us and it.
