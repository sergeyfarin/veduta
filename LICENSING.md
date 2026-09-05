# Licensing

> Not legal advice. Have a lawyer review this before the first public release —
> in particular the plugin exception in §3.

## 1. Layout

Veduta is deliberately **multi-licensed by directory**, following the pattern Grafana used when
it moved its core to AGPLv3 while keeping plugins, agents and SDKs under Apache-2.0.

| Path | License | Why |
| --- | --- | --- |
| `cmd/`, `internal/`, `web/`, everything not listed below | **AGPL-3.0-or-later** | The product. A hosted, closed fork must give its users the source. |
| `sdk/` (plugin SDKs, host bindings, builders) | **Apache-2.0** | A plugin author must never wonder whether using the SDK infects their plugin. |
| `schemas/` (Widget Document, manifest, config) | **Apache-2.0** | Third parties must be free to build compatible tooling, editors and validators. |
| `plugins/` (first-party integrations) | **Apache-2.0** | They are examples that community integrations will be copied from. |
| `examples/`, `docs/` | **Apache-2.0** / CC-BY-4.0 for prose | Copy-paste friendly. |

Use SPDX headers (`// SPDX-License-Identifier: AGPL-3.0-or-later`) on every source file and follow
the [REUSE](https://reuse.software/) spec, so `reuse lint` in CI keeps this honest.

`AGPL-3.0-**or-later**`, not `-only`: it keeps the option of a future AGPLv4 without hunting down
every contributor.

## 2. Contributions

**DCO (`Signed-off-by:`), not a CLA.** A CLA would preserve the option to relicense or dual-license
later, but it adds friction and reads as a prelude to a rug-pull — which is exactly the suspicion an
AGPL homelab project should avoid. The trade-off accepted here: **relicensing later will require the
consent of every contributor.** If a commercial dual-license is a plausible future, decide that now,
because retrofitting a CLA is close to impossible.

## 3. Plugin exception (draft)

Whether a WASM module invoked across a host-function boundary is a derivative work of the host is
unsettled. Rather than let that ambiguity chill the ecosystem, state the intent explicitly. It ships as `LICENSE-PLUGIN-EXCEPTION.txt` next to the verbatim `LICENSE`, and is referenced
from the SPDX headers of every file it covers:

```
ADDITIONAL PERMISSION UNDER GNU AGPL VERSION 3 SECTION 7

The copyright holders of Veduta grant you the additional permission described below.

An "Integration" is a work that interacts with Veduta solely through the Veduta
Integration API: the host functions, the plugin manifest format, and the Widget
Document format, as published in the schemas/ directory of this repository, or
through the declarative integration manifest format.

Integrations are separate and independent works. Loading, running, or distributing
an Integration, whether or not it was compiled against a Veduta SDK, does not by
itself cause that Integration to be a work based on Veduta, and you may license and
distribute Integrations under terms of your choice.

This permission does not extend to works that incorporate, link against, or embed
source code of Veduta itself other than the Integration API definitions and the
SDKs distributed under the Apache License 2.0.
```

This mirrors the Linux syscall note and the GCC runtime exception: the interface is the boundary.

## 4. Obligations this creates on us

- **AGPL §13 (network use).** Anyone interacting with a Veduta instance over a network must be
  offered the corresponding source. Satisfy it in the product, not just the README: a "Source" link
  in the UI footer and in `GET /api/v1/version`, pointing at the exact commit the binary was built
  from. Ship it in milestone A1/A3, not as a release-day afterthought.
- **Third-party notices.** The frontend bundles MIT/BSD/ISC code; generate `THIRD-PARTY-NOTICES.md`
  at build time and serve it from the UI.
- **Configuration is not modification.** Document plainly that writing YAML, declarative manifests
  or themes does not create a modified version of Veduta and triggers no AGPL obligation. Users will
  ask; unanswered, the license becomes a reason not to adopt.
- **Trademark.** The license does not cover the name. If the name matters, publish a short trademark
  policy requiring forks to rebrand.

## 5. Dependency compatibility

All intended dependencies are one-way compatible into AGPLv3: Apache-2.0 (wazero,
santhosh-tekuri/jsonschema), BSD-3-Clause (Extism Go SDK, modernc.org/sqlite, golang.org/x/*,
fsnotify), MIT (expr-lang, yaml.v3, Svelte, Vite), ISC (lucide). **A GPLv2-only dependency would be
incompatible** — add a CI license check (`go-licenses` / `license-checker`) that fails the build on
any GPLv2-only, SSPL, BUSL or unlicensed dependency.
