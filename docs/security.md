# Security model

Veduta treats configuration, credentials, integrations, upstream services, and browsers as separate trust boundaries. This page is the operator summary. [`01-architecture.md`](01-architecture.md) contains the detailed threat model and decisions.

## Access to Veduta

Every production configuration states `auth.mode` explicitly. Password mode stores an Argon2id verifier, creates server-side sessions, rotates CSRF tokens, rate-limits login attempts, and requires a fresh five-minute sudo window for privileged HTTP operations. Forward mode accepts identity headers only from configured proxy addresses; direct clients cannot assert them. Its default `privilegedOperations: cli-only` keeps integration approval at the local CLI unless an administrator deliberately grants configured groups that authority.

Unauthenticated mode binds only to loopback. The `--i-know-what-im-doing` override is required for a non-loopback bind and for configurations containing actions or secrets. Prefer real authentication over the override.

Terminate TLS at a trusted reverse proxy or place Veduta behind a private authenticated network. Configure proxy addresses narrowly, strip client-supplied identity headers at the proxy, and protect the persistent data directory and configuration as sensitive files.

## Credentials and outbound requests

Configuration contains `${secret:NAME}` references. Resolution checks `/run/secrets/NAME` before the process environment. The core stores resolved values in a redacting type and owns authentication injection; integrations receive neither the credential nor the connection URL.

Every outbound request passes three independent checks: the connection's allowed paths, the manifest's requested route, and the administrator's approved lock entry. Veduta canonicalises paths once, allowlists headers and query keys, pins resolved IP addresses to resist DNS rebinding, rechecks redirects, limits response bytes and concurrency, and classifies errors without returning credentials.

Asset references are short-lived signed tokens. The browser receives a Veduta URL, while the core fetches the upstream object through the same connection and route policy. Do not place upstream credentials in card links, markdown, icon URLs, or templates.

## Integration isolation

External integrations start only when the loaded manifest and module match an approved digest. Permission increases or source changes require review and reapproval. Declarative programs have bounded syntax and execution budgets. WASM modules run without WASI and can perform external effects only through the capability broker; memory, fuel, request count, response bytes, and document size are capped.

Integration output is untrusted data. The core validates the Widget Document, the frontend renders a fixed block vocabulary, markdown is parsed into a restricted AST, and unknown block types fail closed. Actions are declared and approved separately from read-only data routes.

## Persistence, events, and notifications

SQLite holds sessions, settings, cached state, event history, rule state, and the notification outbox. Rule state, its transition event, outbox rows, and flood-suspension event commit in one transaction. Notification delivery happens afterward and is at least once, so receivers should tolerate duplicates. Back up the database before upgrades and restrict filesystem access to the Veduta process account.

Logs use structured secret scrubbing, but service payloads can still contain private household data. Control log collection and retention accordingly. Treat configuration, `veduta.lock.yaml`, the database, captured fixtures, and backups as sensitive operational material.

## Deployment checklist

- Use password or correctly configured forward authentication for any non-loopback deployment.
- Put TLS in front of Veduta and allow traffic only from intended networks.
- Keep `trustedProxies` and forward-auth proxy ranges exact; strip identity headers from untrusted requests.
- Mount secrets as files where possible and keep literal secret values out of YAML, lock files, URLs, and logs.
- Review `veduta integration diff` before every approval and reject unexplained routes, body-bearing requests, or raised limits.
- Run with a dedicated user, a writable private data directory, and read-only application/config mounts where practical.
- Keep the binary, web assets, integrations, and lock file from one reviewed release together.
- Back up configuration, the lock file, and SQLite data before upgrades; test restore procedures.
