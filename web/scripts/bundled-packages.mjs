// SPDX-License-Identifier: AGPL-3.0-or-later

// Records which third-party npm packages actually end up inside the shipped bundle, as opposed to
// which ones package.json calls dependencies. Those are different sets here: web/package.json
// declares only devDependencies, yet the compiled output still carries Svelte's runtime. A notices
// file built from the manifest would therefore claim the frontend redistributes nothing, which is
// false. This reads Rollup's real module graph instead.
//
// Output is web/bundled-packages.json, committed and fed to hack/gen-third-party-notices.go. CI
// rebuilds and runs `git diff --exit-code`, so bundling a new library becomes a build failure until
// its notice is regenerated - the same drift rule gen-types.mjs and gen-config-reference.go use.
import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const outputPath = path.resolve(here, '../bundled-packages.json');

// The package a module belongs to is the LAST node_modules/<name> on its path: pnpm resolves
// dependencies through .pnpm/<pkg>@<version>/node_modules/<name>/..., so the first segment is the
// store entry, not the importing package.
function packageRootOf(id) {
  const parts = id.split(path.sep);
  const at = parts.lastIndexOf('node_modules');
  if (at === -1 || at + 1 >= parts.length) return null;
  const first = parts[at + 1];
  if (first === undefined) return null;
  // Scoped packages occupy two segments (@scope/name).
  const span = first.startsWith('@') ? 2 : 1;
  if (at + span >= parts.length) return null;
  return parts.slice(0, at + 1 + span).join(path.sep);
}

export function bundledPackages() {
  return {
    name: 'veduta-bundled-packages',
    apply: 'build',
    generateBundle(_options, bundle) {
      const roots = new Set();
      for (const chunk of Object.values(bundle)) {
        if (chunk.type !== 'chunk') continue;
        for (const id of Object.keys(chunk.modules)) {
          const root = packageRootOf(id);
          if (root) roots.add(root);
        }
      }

      const packages = [];
      for (const root of roots) {
        let meta;
        try {
          meta = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'));
        } catch {
          this.error(`bundled package at ${root} has no readable package.json`);
          return;
        }
        // An unlicensed bundled package is a licence-compliance problem, not a formatting one.
        if (!meta.license) {
          this.error(`bundled package ${meta.name} declares no license`);
          return;
        }
        packages.push({
          name: meta.name,
          version: meta.version,
          license: meta.license,
          homepage: meta.homepage ?? meta.repository?.url ?? null
        });
      }
      packages.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
      writeFileSync(outputPath, JSON.stringify(packages, null, 2) + '\n');
    }
  };
}
