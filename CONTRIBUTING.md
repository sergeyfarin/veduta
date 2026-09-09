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
pnpm dev      # Go API on 127.0.0.1:8099 and Vite on :5173
pnpm check    # go vet, go test, svelte-check — what CI runs
```

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

## Integrations

You do not need to change Veduta to add one. A declarative integration is a single YAML manifest
(`plugins/immich/manifest.yaml` is the worked example). Follow the
[integration authoring guide](docs/integration-authoring.md) and its validation workflow. The
detailed trust model is in [docs/01-architecture.md §5](docs/01-architecture.md): a manifest
*requests* authority and only `veduta.lock.yaml` grants it.
