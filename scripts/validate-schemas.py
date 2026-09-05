#!/usr/bin/env python3
"""Contract checks for Veduta's schemas. Three independent layers:

  1. STRUCTURAL  - every schema is a valid JSON Schema, and every fixture and adversarial
                   case lands on its expected side.
  2. PORTABILITY - every `pattern` compiles under Go's RE2 (no lookahead/backreferences),
                   because the Go validator (santhosh-tekuri/jsonschema) uses stdlib regexp
                   and would otherwise refuse the schema at boot.
  3. CANONICAL   - the manifest digest is RFC 8785 (JCS) over a restricted subset, and Go and
                   Python must agree byte for byte on golden fixtures containing Unicode, <>&,
                   reordered keys and reordered set-like arrays.
  4. SEMANTIC    - scripts/semantic.py, run against the real examples AND against every checked-in
                   negative fixture in testdata/semantic-cases/. Deleting a check therefore fails
                   the suite rather than silently reducing coverage.

Nothing is skipped: an integration referenced by the examples but missing a manifest is an ERROR,
not a pass. Two checks belong to the runtime loader and are documented as out of scope here:
verifying a wasm module file against its declared sha256, and walking rule expressions with the
real expression AST (Spike 3). Until then rule parsing fails closed.

Note on `format` and on patterns: `date-time`/`uri` are ANNOTATIONS in both this validator and the
Go validator's default mode, so nothing relies on them. But a pattern is not a parser either -
`2026-99-99T99:99:99Z` is pattern-shaped and meaningless, and no readable regex validates ports or
bracketed IPv6. So patterns are structural pre-filters (and editor feedback), and layer 3 enforces
semantics with real parsers, mirroring what the Go loader must do. Mirrored by Go table tests once
milestone B2 lands.
"""
import argparse, json, re, subprocess, shutil, sys, pathlib, yaml
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import semantic
from jcs import digest as jcs_digest
from jsonschema import Draft202012Validator, RefResolver

ROOT = pathlib.Path(__file__).resolve().parent.parent
FILES = {
    "widget": "widget-document.v1.schema.json",
    "cardstate": "card-state.v1.schema.json",
    "manifest": "plugin-manifest.v1.schema.json",
    "config": "config.v1.schema.json",
    "lock": "integration-lock.v1.schema.json",
}
S = {n: json.load(open(ROOT / "schemas" / f)) for n, f in FILES.items()}
STORE = {s["$id"]: s for s in S.values()}
fail = 0

def bad(msg):
    global fail
    fail += 1
    print(f"FAIL  {msg}")

def ok(msg):
    print(f"ok    {msg}")


def deep_merge(base, patch):
    """Fixtures express a mutation as a sparse overlay. A null value deletes a key, a list
    replaces wholesale, and __replace__: true makes a map replace instead of merge."""
    if not isinstance(base, dict) or not isinstance(patch, dict):
        return patch
    if patch.get("__replace__"):
        return {k: v for k, v in patch.items() if k != "__replace__"}
    out = dict(base)
    for k, v in patch.items():
        if v is None:
            out.pop(k, None)
        elif isinstance(v, dict) and isinstance(out.get(k), dict):
            out[k] = deep_merge(out[k], v)
        else:
            out[k] = v
    return out

def validator(name):
    return Draft202012Validator(S[name], resolver=RefResolver.from_schema(S[name], store=STORE))

def check(label, name, doc, expect):
    errs = list(validator(name).iter_errors(doc))
    got = "reject" if errs else "accept"
    if got != expect:
        bad(f"{label}: expected {expect}, got {got} {errs[0].message[:80] if errs else ''}")
    else:
        ok(f"{label} ({got})")

# ---------------------------------------------------------------- 1. structural
print("== structural")
for name, schema in S.items():
    try:
        Draft202012Validator.check_schema(schema)
        ok(f"schema {name} is a valid JSON Schema")
    except Exception as e:
        bad(f"schema {name}: {e}")

for path, name in [("examples/veduta.yaml", "config"),
                   ("examples/veduta.lock.yaml", "lock"),
                   ("plugins/immich/manifest.yaml", "manifest"),
                   ("testdata/widgets/jellyfin-recent.golden.json", "widget"),
                   ("testdata/widgets/jellyfin-recent.cardstate.json", "cardstate")]:
    f = ROOT / path
    doc = yaml.safe_load(open(f)) if f.suffix in (".yaml", ".yml") else json.load(open(f))
    check(f"fixture {path}", name, doc, "accept")

for case in json.load(open(ROOT / "testdata/schema-cases.json"))["cases"]:
    check(f"case {case['name']}", case["schema"], case["doc"], case["expect"])

# ---------------------------------------------------------------- 2. portability
print("\n== portability (Go RE2)")
import shutil, subprocess
if shutil.which("go"):
    r = subprocess.run(["go", "run", ".", *[str(ROOT / "schemas" / f) for f in FILES.values()]],
                       cwd=ROOT / "scripts" / "re2check", capture_output=True, text=True)
    for line in (r.stdout + r.stderr).strip().splitlines():
        (ok if r.returncode == 0 else bad)(f"re2check: {line}")
else:
    bad("re2check skipped: no Go toolchain. This layer is not optional - patterns that only "
        "compile in Python would make the Go validator refuse the schema at boot.")

# ---------------------------------------------------------------- Go helpers
GOCHECK = ROOT / "scripts" / "gocheck"
HAVE_GO = shutil.which("go") is not None

def gocheck(sub, *args):
    r = subprocess.run(["go", "run", ".", sub, *args], cwd=GOCHECK, capture_output=True, text=True)
    if r.returncode != 0 and sub != "digest":
        bad(f"gocheck {sub} failed: {r.stderr.strip()[:200]}")
        return []
    return r.stdout.strip().splitlines()

_MT_CACHE, _SV_CACHE = {}, {}

def go_media_type(v):
    if v not in _MT_CACHE:
        out = gocheck("mediatype", v)
        _MT_CACHE[v] = None if not out or out[0].startswith("ERROR") else out[0]
    return _MT_CACHE[v]

def go_semver(v):
    if v not in _SV_CACHE:
        out = gocheck("semver", v)
        _SV_CACHE[v] = bool(out) and out[0] == "true"
    return _SV_CACHE[v]

HELPERS = semantic.Helpers(media_type=go_media_type, semver=go_semver) if HAVE_GO else None

# ---------------------------------------------------------------- 3. canonical digest
print("\n== canonical digest (RFC 8785, Go and Python must agree)")
expected = json.load(open(ROOT / "testdata/canonical/expected-digests.json"))
py_digests = {}
for name, want in sorted(expected["digests"].items()):
    got = jcs_digest(json.load(open(ROOT / f"testdata/canonical/{name}.json")))
    py_digests[name] = got
    (ok if got == want else bad)(f"python digest {name}: {got[:16]}…"
                                 + ("" if got == want else f" != expected {want[:16]}…"))
for name, want in sorted(expected["manifests"].items()):
    got = jcs_digest(yaml.safe_load(open(ROOT / f"plugins/{name}/manifest.yaml")))
    py_digests[name] = got
    (ok if got == want else bad)(f"python digest manifest {name}: {got[:16]}…"
                                 + ("" if got == want else " != expected"))
for a, b in expected["must_match"]:
    (ok if py_digests[a] == py_digests[b] else bad)(
        f"invariant: {a} and {b} normalise to the same digest")
for a, b in expected["must_differ"]:
    (ok if py_digests[a] != py_digests[b] else bad)(
        f"invariant: {a} and {b} must NOT collide (normalisation is path-aware, "
        f"so arbitrary user data named 'capabilities' or 'queryKeys' is left alone)")

if HAVE_GO:
    # The real YAML manifests go through the GO loader too - strict decode, duplicate-key
    # rejection, YAML scalar typing - so this proves agreement of the production path, not just
    # of two JSON canonicalisers.
    files = ([str(ROOT / f"testdata/canonical/{n}.json") for n in sorted(expected["digests"])]
             + [str(ROOT / f"plugins/{n}/manifest.yaml") for n in sorted(expected["manifests"])])
    for line in gocheck("digest", *files):
        if line.startswith("ERROR"):
            bad(f"gocheck digest: {line}")
            continue
        d, f = line.split("  ", 1)
        name = pathlib.Path(f).stem
        if name == "manifest":
            name = pathlib.Path(f).parent.name
        want = py_digests.get(name)
        (ok if d == want else bad)(f"go digest {name}: {d[:16]}…"
                                   + ("" if d == want else f" != python {str(want)[:16]}…"))
    ok("go media-type and SemVer helpers back the semantic layer (mime.ParseMediaType, SemVer 2.0.0)")
else:
    bad("gocheck skipped: no Go toolchain. Cross-language agreement is the whole point of this layer.")

# ---------------------------------------------------------------- 4. semantic
print("\n== semantic (scripts/semantic.py, run on real examples and negative fixtures)")

class DupKeyLoader(yaml.SafeLoader):
    """Rejects duplicate mapping keys, which YAML silently accepts and which are a documented
    smuggling vector for approval diffs."""

def _no_dupes(loader, node, deep=False):
    mapping = {}
    for kn, vn in node.value:
        k = loader.construct_object(kn, deep=deep)
        if k in mapping:
            raise yaml.constructor.ConstructorError(
                None, None, f"duplicate mapping key {k!r}", kn.start_mark)
        mapping[k] = loader.construct_object(vn, deep=deep)
    return mapping

DupKeyLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, _no_dupes)

def load_yaml(path):
    with open(path) as f:
        return yaml.load(f, Loader=DupKeyLoader)

SCHEMA_LIMITS = S["manifest"]["$defs"]["limits"]
manifests = {}
for f in sorted((ROOT / "plugins").glob("*/manifest.yaml")):
    try:
        m = load_yaml(f)
    except yaml.YAMLError as e:
        bad(f"{f}: {e}")
        continue
    manifests[m["metadata"]["id"]] = m
    check(f"fixture {f.relative_to(ROOT)}", "manifest", m, "accept")

lock = load_yaml(ROOT / "examples/veduta.lock.yaml")
cfg = load_yaml(ROOT / "examples/veduta.yaml")
problems = semantic.issues_for(manifests, lock, cfg, SCHEMA_LIMITS, HELPERS)
problems += semantic.cardstate_issues('fixture cardstate',
    json.load(open(ROOT / 'testdata/widgets/jellyfin-recent.cardstate.json')))
for pr in problems:
    bad(pr)
if not problems:
    ok(f"real examples: {len(manifests)} manifests, {len(lock['integrations'])} lock entries, "
       f"{sum(len(s.get('cards', [])) for s in cfg.get('sections', []))} cards, "
       f"{len(cfg.get('rules', []))} rules - no skips, every reference resolves")

cases_dir = ROOT / "testdata/semantic-cases"
for f in sorted(cases_dir.glob("*.yaml")):
    case = load_yaml(f)
    ms = dict(manifests)
    for iid, patch in (case.get("manifests") or {}).items():
        ms[iid] = deep_merge(ms.get(iid, {}), patch)
    lk = deep_merge(lock, case.get("lock") or {})
    cf = deep_merge(cfg, case.get("config") or {})
    found = semantic.issues_for(ms, lk, cf, SCHEMA_LIMITS, HELPERS)
    for cs in (case.get('cardstates') or []):
        found += semantic.cardstate_issues('cardstate', cs)
    if case["expect"] == "__NONE__":
        (ok if not found else bad)(
            f"positive fixture {f.name}: must produce no issues"
            + ("" if not found else f" but got {found[:2]}"))
    elif any(case["expect"] in p for p in found):
        ok(f"negative fixture {f.name}: caught {case['expect']!r}")
    else:
        bad(f"negative fixture {f.name}: expected an issue containing {case['expect']!r}, got "
            + (f"{found[:2]}" if found else "NO ISSUES - the check it guards may have been deleted"))

print(f"\n{'FAILED' if fail else 'PASSED'}: {fail} failure(s)")
sys.exit(1 if fail else 0)
