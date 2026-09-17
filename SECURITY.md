# Security policy

Veduta's whole design argument is a boundary: an integration describes what it wants, the core
decides whether it is allowed, and the core holds every credential. A report that the boundary does
not hold where it claims to is the most useful thing anyone can send this project.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: the **Security** tab of this repository →
**Report a vulnerability**. That channel is private until an advisory is published, keeps the
report attached to the fix, and is deliberately the only one — a personal mailbox published in a
file this prominent gets scraped, and a shared inbox would be theatre for a single-maintainer
project.

If you cannot use it, open a normal issue asking for a private channel **with no details of the
finding in it**, and you will get one.

Veduta is pre-1.0 and maintained by one person in their own time. Expect acknowledgement within a
week rather than within hours. If a report is a real boundary failure it is worked on before
features; if it is a deployment mistake or an accepted limit, you will get a straight answer saying
so and a pointer to where that is written down.

## Supported versions

No version has been released yet. `0.1.0` is built and verified but the tag is deliberately
uncut, so **`main` is the only line that receives fixes** — there is nothing older to back-port to.
Once 0.1.0 is tagged, security fixes land on `main` and in the next patch release, and anything
warranting an advisory gets one on the Security tab.

Note the pre-1.0 carve-out in [CHANGELOG.md](CHANGELOG.md): the plugin ABI, host functions and
manifest schema are not under semantic versioning yet. A security fix is allowed to break them,
which can mean rebuilding and re-approving third-party plugins.

## What is in scope

The controls below are claims the code makes, each with tests behind it. A way around any of them
is a vulnerability, not a feature request:

- **Sandbox escape** — a WASM module reaching the filesystem, environment, sockets, native HTTP or
  WASI, or surviving its deadline, memory cap or output cap.
- **Broker bypass** — any outbound request that was not checked against all three of the
  connection's policy, the manifest's declared route and the administrator's approved lock entry;
  or an `assets.ref` minted against a route that is not `use: asset`.
- **Credential disclosure** — a secret, or a connection URL, reaching an integration, an HTTP
  response, a log line, an asset token or the lock file. Plugins never receiving secrets is a
  boundary; the redacting type and the log scrubber are defence in depth behind it.
- **Approval bypass** — widening an integration's authority without an administrator's grant:
  manifest or module swaps that do not force re-approval, digest-binding or TOCTOU failures in the
  approve flow, or privileged operations without a fresh sudo window where one is required.
- **Authentication and session flaws** — session fixation or forgery, CSRF, sudo-window bypass,
  forward-auth identity headers accepted from an unconfigured address, rate-limit bypass on login.
- **SSRF and request forgery** — reaching a host or path that no route grants, DNS-rebinding
  windows, redirect handling, path-canonicalisation mismatches between the approval check and the
  request, header or query injection into an authenticated upstream request.
- **Asset and icon proxy abuse** — token forgery or replay, using either endpoint as an open proxy.
- **Untrusted content reaching the browser** — anything that gets markup, script or a `javascript:`
  URL past the Widget Document validator, the fixed block vocabulary, the restricted markdown AST
  or the CSP. The codebase contains zero `{@html}` and CI enforces that; a regression is in scope.
- **Persistence and notification integrity** — reading or writing another session's rows, or
  defeating the rule debounce, dedupe, rate limits and auto-suspension that stop a flapping rule
  from becoming a flood.

[docs/security.md](docs/security.md) is the operator summary of these, and
[docs/01-architecture.md §8](docs/01-architecture.md) is the threat-model table they come from.

## Known limits, already written down

These are documented design decisions or accepted gaps, not findings. A report that one of them
exists will be closed with a pointer here; a report that one is *worse than documented* is welcome:

- **Route grants are `(slot, method, path)` and do not read bodies.** A `POST` route approved for
  queries can carry something else — `queryKeys`, `contentType` and `maxBodyKB` narrow it, body
  schemas are a later `apiVersion`, and non-GET routes are flagged at approval time.
- **Secret scanning is explicitly not a control.** Encoding or chunking defeats it. It exists to
  catch accidents, and the boundary is that plugins never receive secrets in the first place.
- **Plugin modules are pinned by sha256 but not signed.** Signature verification is designed for
  0.3. Until then, an upgrade is a digest change that forces a re-approval with a printed diff.
- **Actions are not executable in 0.1.** Declared action buttons render permanently disabled, so
  there is no action authorisation path to attack yet.
- **Whoever can edit `veduta.yaml` or `veduta.lock.yaml` is an administrator.** Config is a trust
  boundary by design: it defines connections and holds credential references.
- **An approved integration can spend what it was granted.** Budgets, per-plugin concurrency and
  the circuit breaker bound it; they do not make a hostile-but-approved plugin harmless. Review the
  diff before approving.
- **`auth: none` and `--i-know-what-im-doing` do what they say.** Running Veduta unauthenticated off
  loopback, without TLS, or with a wide `trustedProxies` range is an operator decision, and the
  deployment checklist in [docs/security.md](docs/security.md) is the countermeasure.
- **32-bit `linux/arm/v7` WASM performance is unmeasured**, as recorded in
  [docs/03-backlog.md](docs/03-backlog.md). It is a support-claim gap, not a security one.

## Disclosure

Coordinated: report privately, and please give roughly 90 days before publishing, or less by
agreement if a fix ships sooner. Reporters are credited in the advisory and the changelog unless
they ask not to be. There is no bounty programme, and no report is treated as less valid for the
absence of one.
