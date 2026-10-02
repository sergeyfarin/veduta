# Contributing

## Sign-off, not a CLA

Every commit needs a Developer Certificate of Origin sign-off:

```bash
git commit -s -m "..."
```

which appends `Signed-off-by: Your Name <you@example.com>`, certifying you wrote the change or
have the right to submit it under the project's licence ([developercertificate.org](https://developercertificate.org/)).

There is deliberately **no CLA**. A CLA would keep the option of relicensing later, but it adds
friction and reads as a prelude to a rug-pull — which is exactly the suspicion an AGPL project
should not invite. The trade-off is accepted knowingly: relicensing would require every
contributor's consent. See [LICENSING.md](LICENSING.md).

## Licences by directory

| Path | Licence |
| --- | --- |
| `cmd/`, `internal/`, `web/` | AGPL-3.0-or-later |
| `sdk/`, `schemas/`, `plugins/`, `examples/` | Apache-2.0 |

Every source file carries an `SPDX-License-Identifier` header, and a test enforces it. Integrations
that talk to Veduta only through the published Integration API are not derived works — see
[LICENSE-PLUGIN-EXCEPTION.txt](LICENSE-PLUGIN-EXCEPTION.txt).

## Getting set up

```bash
mise install && pnpm install
pnpm dev        # Go API on 127.0.0.1:8099 and Vite on :5173
mise run check  # everything CI runs, including the linter
```

`mise run check` is the one to run before pushing. `pnpm check` is a faster subset for the inner
loop — go vet, go test, svelte-check, vitest — and it deliberately leaves out `golangci-lint`,
which CI runs as a separate job and which catches a class of thing `go vet` does not
(`.golangci.yml` adds gosec, revive, errorlint, gocritic, usestdlibvars and more). A change can
pass `pnpm check` and still fail CI on lint alone. golangci-lint is pinned in `mise.toml` and
resolves only through mise, so reach it via `mise run lint` rather than calling it directly.

## What a change needs

- **A test that proves the denial, not just the happy path.** A capability check without a
  negative test is not a capability check. This is the project's central claim, so it is the
  standard applied to reviews.
- **Contract changes come with fixtures.** Schemas live in `schemas/`, and the suite in
  `internal/contracts` runs the adversarial corpus (`testdata/schema-cases.json`) and the
  cross-document fixtures (`testdata/semantic-cases/`). If you add a check, add the negative
  fixture that fails when someone deletes it.
- **New dependencies are a discussion.** The budget is deliberately about ten direct Go modules
  (see [docs/00-review-and-prior-art.md](docs/00-review-and-prior-art.md)). Anything added must be
  recorded in [THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md), which a test also enforces.
- **Frontend versions are exact.** `.npmrc` sets `save-exact`; do not reintroduce `^` ranges.

## Tracking work

Open gaps, deferred decisions and planned work are [GitHub issues](https://github.com/sergeyfarin/veduta/issues).
Anything noticed while working on something else and not fixed in the same change gets an issue
before the change lands: what the gap is, where it was found, why it was not fixed there, and a
`priority:` label. Deliberate decisions not to build something stay open with the `deferred` label
and say what would trigger a revisit, so the reasoning is not relitigated. Close an issue with a
comment naming the commit or release that fixed it.

Larger design work goes in [docs/decisions/](docs/decisions/) (accepted) or
[docs/proposals/](docs/proposals/) (not yet accepted). Finished planning records live in
[docs/archive/](docs/archive/README.md).

## Integrations

You do not need to change Veduta to add one. A declarative integration is a single YAML manifest
(`plugins/immich/manifest.yaml` is the worked example). Follow the
[integration authoring guide](docs/integration-authoring.md) and its validation workflow. The
detailed trust model is in [docs/01-architecture.md §5](docs/01-architecture.md): a manifest
*requests* authority and only `veduta.lock.yaml` grants it.

## Releasing

Unreleased changes collect under `## Unreleased` in [CHANGELOG.md](CHANGELOG.md). To cut a release:

1. Rename that heading to the version and date, and name the new tag in `README.md`,
   `compose.yaml`, `docs/getting-started.md`, `docs/docker.md` and `SECURITY.md` ("`X.Y.Z` is the
   current release"). A contract test fails until all of them agree with the changelog.
2. Push to `main` and wait for CI to pass on that exact commit: the release workflow's gate job
   refuses a tag whose commit has no green CI run.
3. Push the tag (`vX.Y.Z`). `release.yml` runs the README quick start against the built image,
   then publishes the archives, checksums, GHCR images, provenance, SBOM and GitHub release.

`:latest` is published only from v1.0.0, and every pre-1.0 or prerelease-tagged build is marked a
GitHub pre-release; both read the same condition in `release.yml`. This is permanent, not an alpha
special case.
