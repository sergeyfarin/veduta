#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-or-later

// Generates web/src/lib/types/{widget,cardstate}.ts from the JSON Schemas in schemas/, so the Go
// and TypeScript types come from ONE source of truth rather than being kept in sync by hand.
//
// `pnpm gen-types` regenerates; CI runs it and fails the build with `git diff --exit-code` if the
// checked-in files differ from what regeneration produces - drift becomes a build failure, not
// something noticed later. See internal/widgets and internal/state (the Go half) and
// docs/02-implementation-plan.md milestone B2.
import { compile } from 'json-schema-to-typescript';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const schemasDir = path.resolve(here, '../../schemas');
const outDir = path.resolve(here, '../src/lib/types');

const banner = (source) => `/**
 * GENERATED FILE - do not edit by hand.
 *
 * Produced by web/scripts/gen-types.mjs from ${source}.
 * Run \`pnpm gen-types\` to regenerate. CI fails if this file would change.
 */
`;

async function readSchema(filename) {
  return JSON.parse(await readFile(path.join(schemasDir, filename), 'utf8'));
}

const widgetDocumentSchema = await readSchema('widget-document.v1.schema.json');

// Every schema in this repo is designed to be loaded standalone by the Go side too (via the full
// https:// $id, see schemas/embed.go), so card-state's $ref to widget-document is an absolute
// URL, not a relative path. This resolver points that URL at the already-loaded local file
// instead of letting the ref-parser try to fetch it over the network.
const localRefResolver = {
  order: 1,
  canRead: (file) => file.url === widgetDocumentSchema.$id,
  read: () => JSON.stringify(widgetDocumentSchema)
};

const commonOptions = {
  cwd: schemasDir,
  additionalProperties: false,
  style: { singleQuote: true },
  // Bounded arrays (blocks: maxItems 12, table rows: maxItems 100, ...) would otherwise compile
  // to a union of every fixed-length tuple from 0 to the max - technically faithful to the
  // schema, unusable as a type. A plain T[] is what every caller actually wants; the length
  // limit is enforced by internal/widgets.Validate on the Go side, not by the type.
  ignoreMinAndMaxItems: true,
  $refOptions: { resolve: { veduta: localRefResolver, http: false } }
};

async function generateWidget() {
  const ts = await compile(widgetDocumentSchema, 'WidgetDocument', {
    ...commonOptions,
    bannerComment: banner('schemas/widget-document.v1.schema.json'),
    // schema.title ("Veduta Widget Document v1") would otherwise name the root export.
    customName: (s) => (s.$id === widgetDocumentSchema.$id ? 'WidgetDocument' : undefined)
  });
  await mkdir(outDir, { recursive: true });
  await writeFile(path.join(outDir, 'widget.ts'), ts);
  console.log('wrote web/src/lib/types/widget.ts');
}

// card-state.v1's cross-field rules (§4 of docs/01-architecture.md: "legal state combinations")
// are expressed as five `allOf`/`if`/`then` branches, one per ExecutionState. That is the right
// way to write the JSON Schema - it is what lets a single document be validated against all five
// rules at once - but json-schema-to-typescript compiles allOf into a TypeScript INTERSECTION of
// all five branches, not a union keyed on the discriminant. That is not merely inconvenient, it
// is WRONG for a discriminated type: every field from every state becomes simultaneously present
// and optional, and TypeScript can no longer narrow `document` to non-null when
// `execution.state === 'ok'`, which is the entire point of the type existing.
//
// So this schema's root is hand-assembled here instead, reading the same allOf branches
// programmatically (never hardcoded independently of the file) and reusing compile() for every
// sub-shape it DOES handle correctly (Source, RunError). If the schema's branch structure ever
// changes in a way this does not understand, the assertions below fail loudly rather than
// silently emitting a wrong type - the same trade-off internal/state/cardstate.go's Go
// constructors already make, since Go has no schema-driven codegen for this either.
async function generateCardState() {
  const schema = await readSchema('card-state.v1.schema.json');
  const execBase = schema.properties.execution.properties;

  const stateSchema = { type: 'string', enum: Object.keys(execBase.state.enum ? {} : {}) };
  const executionStates = execBase.state.enum;
  if (!Array.isArray(executionStates) || executionStates.length !== 5) {
    throw new Error(
      `expected exactly 5 execution states in card-state.v1.schema.json, found: ${JSON.stringify(executionStates)}`
    );
  }

  const sourceTS = await compile(execBase.source, 'Source', {
    ...commonOptions,
    bannerComment: ''
  });
  const errorTS = await compile(
    { ...execBase.error, type: 'object' }, // strip the schema's ["object","null"] - null is handled by the union, not this sub-type
    'RunError',
    { ...commonOptions, bannerComment: '' }
  );

  // Every property name that can appear under `execution`, excluding the discriminant itself.
  const allExecProps = Object.keys(execBase).filter((k) => k !== 'state');
  const propType = {
    generatedAt: 'string',
    ttlSeconds: 'number',
    expiresAt: 'string',
    staleSince: 'string',
    durationMs: 'number',
    consecutiveFailures: 'number',
    nextRunAt: 'string',
    source: 'Source',
    error: 'RunError | null',
    disabledReason: execBase.disabledReason.enum.map((v) => `'${v}'`).join(' | '),
    circuitOpenUntil: 'string'
  };
  for (const p of allExecProps) {
    if (!(p in propType)) throw new Error(`gen-types.mjs does not know the TS type for execution.${p}`);
  }

  const branches = schema.allOf.map((branch) => {
    const state = branch.if.properties.execution.properties.state.const;
    const then = branch.then;
    const execThen = then.properties?.execution ?? {};
    const removed = new Set(
      Object.entries(execThen.properties ?? {})
        .filter(([, v]) => v === false)
        .map(([k]) => k)
    );
    const required = new Set(execThen.required ?? []);
    const docType =
      then.properties?.document?.type === 'null'
        ? 'null'
        : then.properties?.document?.type === 'object'
          ? 'WidgetDocument'
          : 'WidgetDocument | null'; // not narrowed by this branch (disabled): either is legal

  // KNOWN LIMITATION: a branch that forces a field to a specific value while KEEPING it present
  // (card-state.v1's `error: {"type":"null"}` on the "ok" branch, forcing error to null without
  // removing the property) is not distinguished here from a field that is merely optional - both
  // end up typed as the field's general type (e.g. `error?: RunError | null`), not narrowed to
  // exactly `null`. Only outright REMOVAL (`false`) is modelled precisely. This makes the
  // generated types slightly more PERMISSIVE than the schema in a few spots, never more
  // restrictive - and the frontend only ever CONSUMES a CardState the backend already built via
  // the airtight Go constructors in internal/state/cardstate.go, never constructs one itself, so
  // the practical risk of this gap is low. Tightening it further is possible but was judged not
  // worth its complexity for a read-only consumer; revisit if that assumption stops holding.
    const docRequired = (then.required ?? []).includes('document');

    const fields = allExecProps
      .filter((p) => !removed.has(p))
      .map((p) => `  ${p}${required.has(p) ? '' : '?'}: ${propType[p]};`)
      .join('\n');

    const name = 'Execution' + state[0].toUpperCase() + state.slice(1);
    return `export interface ${name} {
  state: '${state}';
${fields}
}

export interface CardState${name.slice('Execution'.length)} {
  cardId: string;
  document${docRequired ? '' : '?'}: ${docType};
  execution: ${name};
}
`;
  });

  const variantName = (state) => 'CardState' + state[0].toUpperCase() + state.slice(1);

  // TypeScript does not narrow an OUTER union based on a discriminant nested one level down
  // (`cs.execution.state`, not `cs.state`) - a real, documented limitation, not a bug in this
  // generator; confirmed empirically against this exact shape before adding these. The wire
  // format genuinely nests state under execution (matching the Go and JSON Schema shape), and
  // that shape is not changed just to make narrowing convenient - so callers narrow through
  // these generated type-guard predicates instead of a direct `cs.execution.state === 'x'` check.
  const guards = executionStates
    .map(
      (st) => `export function is${st[0].toUpperCase()}${st.slice(1)}(cs: CardState): cs is ${variantName(st)} {
  return cs.execution.state === '${st}';
}`
    )
    .join('\n\n');

  const out = `${banner('schemas/card-state.v1.schema.json')}
import type { WidgetDocument } from './widget';

${sourceTS}
${errorTS}
${branches.join('\n')}
/**
 * ${schema.description}
 */
export type CardState = ${executionStates.map(variantName).join(' | ')};

${guards}
`;

  await mkdir(outDir, { recursive: true });
  await writeFile(path.join(outDir, 'cardstate.ts'), out);
  console.log('wrote web/src/lib/types/cardstate.ts');
}

await generateWidget();
await generateCardState();
