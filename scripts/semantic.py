"""Cross-document semantic checks, as pure functions over already-parsed documents.

Separated from the runner so the SAME code validates the real examples and every checked-in
negative fixture in testdata/semantic-cases/. Nothing here is "mutation-proven by hand": if a
check is deleted, its negative fixture fails the suite.

This mirrors the checks the Go loader must perform. Two are deliberately out of scope here and
belong to the runtime loader: verifying a wasm module file against its declared sha256, and
walking rule expressions with the real expression AST once Spike 3 chooses the language. Until
then rule parsing here is strict and FAILS CLOSED - see check_rules.
"""
import datetime
import re
import urllib.parse

CORE_DEFAULTS = {}          # filled from the manifest schema by the runner
CORE_MAXIMA = {}


class Helpers:
    """Go-backed helpers. Media-type normalisation and SemVer validity are delegated to
    scripts/gocheck so the checker exercises mime.ParseMediaType and the real SemVer rules
    rather than a Python approximation that would drift. Absent helpers fail closed."""

    def __init__(self, media_type=None, semver=None):
        self._media_type = media_type
        self._semver = semver

    def media_type(self, v):
        if self._media_type is None:
            raise RuntimeError("media-type helper unavailable")
        return self._media_type(v)

    def semver(self, v):
        if self._semver is None:
            raise RuntimeError("semver helper unavailable")
        return self._semver(v)


def issues_for(manifests, lock, cfg, schema_limits, helpers=None):
    """Returns a list of human-readable problems. Empty means clean."""
    out = []
    bad = out.append
    if helpers is None:
        bad("semantic layer ran without Go helpers: media-type and SemVer checks would be "
            "Python approximations, so the run fails closed")
        return out
    defaults = {k: v.get("default") for k, v in schema_limits["properties"].items()}
    maxima = {k: v.get("maximum") for k, v in schema_limits["properties"].items()}

    # ---------------- value semantics (parsers, not patterns) ----------------
    TIME_KEYS = ("generatedAt", "expiresAt", "staleSince", "nextRunAt", "circuitOpenUntil",
                 "approvedAt", "since", "timestamp", "at")
    URL_KEYS = ("url", "link", "homepage")

    def walk(node, path=""):
        if isinstance(node, dict):
            for k, v in node.items():
                yield from walk(v, "%s.%s" % (path, k))
        elif isinstance(node, list):
            for i, v in enumerate(node):
                yield from walk(v, "%s[%d]" % (path, i))
        elif isinstance(node, str):
            yield path, node

    def is_rfc3339(v):
        try:
            datetime.datetime.fromisoformat(v.replace("Z", "+00:00"))
            return True
        except Exception:
            return False

    def parse_url(v):
        try:
            return urllib.parse.urlsplit(v)
        except ValueError:
            return None

    def usable_http_url(v):
        u = parse_url(v)
        if u is None or u.scheme not in ("http", "https") or not u.hostname:
            return False
        try:
            return u.port is None or 1 <= u.port <= 65535
        except ValueError:
            return False

    def scan(label, doc):
        for path, value in walk(doc):
            key = path.rsplit(".", 1)[-1].split("[")[0]
            if key in TIME_KEYS and not is_rfc3339(value):
                bad("%s%s: %r is pattern-shaped but not a real timestamp" % (label, path, value))
            if key in URL_KEYS and not usable_http_url(value):
                bad("%s%s: %r is not a usable http(s) URL" % (label, path, value))

    def dupes(seq):
        seen, out2 = set(), set()
        for x in seq:
            (out2 if x in seen else seen).add(x)
        return sorted(out2)

    # ---------------- route identity ----------------
    def norm_content_type(ct):
        """Delegated to mime.ParseMediaType via gocheck: naive semicolon splitting is wrong for
        quoted parameter values such as profile="a;b" and for escaped quotes."""
        if not ct:
            return None
        n = helpers.media_type(ct)
        if n is None:
            bad("invalid media type %r" % ct)
            return ("<invalid>", ct)
        return n

    def route_identity(r, body_kb):
        return (r["slot"], r["method"], r["path"], r.get("use", "data"),
                tuple(sorted(set(r["queryKeys"]))) if "queryKeys" in r else None,
                norm_content_type(r.get("contentType")),
                r.get("maxBodyKB", body_kb))

    def body_ceiling(spec):
        return spec.get("limits", {}).get("requestBodyKB", defaults["requestBodyKB"])

    # ---------------- manifests ----------------
    for iid, man in sorted(manifests.items()):
        scan("manifest/%s" % iid, man)
        spec = man["spec"]
        if not helpers.semver(man["metadata"]["version"]):
            bad("manifest/%s: version %r is not valid SemVer 2.0.0"
                % (iid, man["metadata"]["version"]))
        slots = {s["name"]: s for s in spec.get("slots", [])}
        caps = set(spec["capabilities"])
        body_kb = body_ceiling(spec)
        for d in dupes([s["name"] for s in spec.get("slots", [])]):
            bad("%s: duplicate slot name %r" % (iid, d))
        for d in dupes([o["id"] for o in spec["operations"]]):
            bad("%s: duplicate operation id %r" % (iid, d))
        for k, v in (spec.get("limits") or {}).items():
            if maxima.get(k) is not None and v > maxima[k]:
                bad("%s: limit %s=%s exceeds the core maximum %s" % (iid, k, v, maxima[k]))
        for op in spec["operations"]:
            oid = "%s/%s" % (iid, op["id"])
            declared = {s["name"] for s in op.get("signals", [])}
            routes = op.get("routes", [])
            for d in dupes([s["name"] for s in op.get("signals", [])]):
                bad("%s: duplicate signal declaration %r" % (oid, d))
            for d in dupes([route_identity(r, body_kb) for r in routes]):
                bad("%s: duplicate route %s" % (oid, d))
            if any(r.get("use", "data") == "data" for r in routes) and "http" not in caps:
                bad("%s: has data routes but does not request the http capability" % oid)
            if any(r.get("use") == "asset" for r in routes) and "assets" not in caps:
                bad("%s: has asset routes but does not request the assets capability" % oid)
            for r in routes:
                if r["slot"] not in slots:
                    bad("%s: route references undeclared slot %r" % (oid, r["slot"]))
                if r.get("maxBodyKB", body_kb) > body_kb:
                    bad("%s: route maxBodyKB widens the manifest requestBodyKB" % oid)
            # pipeline requests must be COVERED BY A DECLARED ROUTE, not merely use a known slot
            for step in op.get("pipeline", []):
                req = step["request"]
                if req["slot"] not in slots:
                    bad("%s: pipeline references undeclared slot %r" % (oid, req["slot"]))
                    continue
                method = req.get("method", "GET")
                path = req["path"]
                if isinstance(path, dict):
                    bad("%s: pipeline path is an expression (%r); v1 requires literal paths so "
                        "route coverage is decidable at load time" % (oid, path))
                    continue
                candidates = [r for r in routes
                              if r["slot"] == req["slot"] and r["method"] == method
                              and _glob_match(r["path"], path)
                              and r.get("use", "data") == "data"]
                if not candidates:
                    bad("%s: pipeline request %s %s is not covered by any declared route"
                        % (oid, method, path))
                    continue
                # Every constraint that is statically decidable is decided here; anything
                # dynamic is explicitly deferred to the broker at runtime.
                sent_keys = set((req.get("query") or {}).keys())
                body = req.get("body") or {}
                body_kind = "json" if "json" in body else ("form" if "form" in body else None)
                reasons, satisfied = [], False
                for r in candidates:
                    if "queryKeys" in r and not sent_keys <= set(r["queryKeys"]):
                        reasons.append("sends query keys %s outside the route allowlist %s"
                                       % (sorted(sent_keys - set(r["queryKeys"])),
                                          sorted(r["queryKeys"])))
                        continue
                    if body_kind:
                        ct = r.get("contentType")
                        if not ct:
                            reasons.append("sends a %s body but the route declares no contentType"
                                           % body_kind)
                            continue
                        n = norm_content_type(ct)
                        base = n.split(";")[0] if isinstance(n, str) else ""
                        want_json = base == "application/json" or base.endswith("+json")
                        want_form = base == "application/x-www-form-urlencoded"
                        if body_kind == "json" and not want_json:
                            reasons.append("sends a json body but the route declares %r" % ct)
                            continue
                        if body_kind == "form" and not want_form:
                            reasons.append("sends a form body but the route declares %r" % ct)
                            continue
                    satisfied = True
                    break
                if not satisfied:
                    bad("%s: pipeline request %s %s matches a route path but violates its "
                        "constraints: %s" % (oid, method, path, "; ".join(reasons)))
            emitted = set((op.get("output") or {}).get("signals", {}).keys())
            if emitted - declared:
                bad("%s: emits signals not declared by THIS operation: %s"
                    % (oid, sorted(emitted - declared)))
            for node in _asset_nodes(op.get("output") or {}):
                slot = node.get("slot")
                if not any(r["slot"] == slot and r.get("use") == "asset" for r in routes):
                    bad("%s: asset node for slot %r has no use=asset route" % (oid, slot))

    # ---------------- lock vs manifests ----------------
    scan("lock", lock)
    for iid, entry in sorted(lock["integrations"].items()):
        if iid not in manifests:
            bad("lock/%s: no manifest for this integration - the example set must be complete, "
                "a skipped entry is not a passing check" % iid)
            continue
        man = manifests[iid]
        spec = man["spec"]
        body_kb = body_ceiling(spec)
        requested = {route_identity(r, body_kb)
                     for op in spec["operations"] for r in op.get("routes", [])}
        approved = {route_identity(r, body_kb) for r in entry["routes"]}
        for extra in sorted(approved - requested):
            bad("lock/%s: approves a route the manifest never requested (full identity): %s"
                % (iid, extra))
        for cap in sorted(set(entry["capabilities"]) - set(spec["capabilities"])):
            bad("lock/%s: approves capability %r the manifest never requested" % (iid, cap))
        if entry.get("runtime") and entry["runtime"] != spec["runtime"]:
            bad("lock/%s: records runtime %r but the manifest declares %r"
                % (iid, entry["runtime"], spec["runtime"]))
        # limits: compare against manifest value-or-DEFAULT, never against "absent means anything"
        for k, v in (entry.get("limits") or {}).items():
            req = spec.get("limits", {}).get(k, defaults.get(k))
            if req is not None and v > req:
                bad("lock/%s: limit %s=%s exceeds the manifest's effective request %s "
                    "(manifest value or documented default)" % (iid, k, v, req))
        eff = entry.get("effectiveLimits")
        all_keys = set(defaults)
        if not eff:
            bad("lock/%s: effectiveLimits is missing; without it a change to a core default "
                "silently moves authority instead of forcing re-approval" % iid)
        else:
            missing = all_keys - set(eff)
            unknown = set(eff) - all_keys
            if missing:
                bad("lock/%s: effectiveLimits is incomplete, missing %s" % (iid, sorted(missing)))
            if unknown:
                bad("lock/%s: effectiveLimits has unknown keys %s" % (iid, sorted(unknown)))
            for k, v in eff.items():
                if k not in all_keys:
                    continue
                expect = min(x for x in (maxima.get(k),
                                         spec.get("limits", {}).get(k, defaults.get(k)),
                                         (entry.get("limits") or {}).get(k, defaults.get(k)))
                             if x is not None)
                if v != expect:
                    bad("lock/%s: effectiveLimits.%s=%s but min(core, manifest, approved)=%s"
                        % (iid, k, v, expect))
        from jcs import digest
        computed = digest(man)
        if entry["manifestSha256"] != computed:
            bad("lock/%s: manifestSha256 %s… does not match the canonical digest %s…"
                % (iid, entry["manifestSha256"][:12], computed[:12]))

    # ---------------- configuration ----------------
    scan("config", cfg)
    conns = cfg.get("connections") or {}
    for cid, conn in conns.items():
        if conn["kind"] != "http":
            continue
        u = parse_url(conn["baseUrl"])
        if u is None or not usable_http_url(conn["baseUrl"]):
            bad("connection %s: baseUrl %r is not a usable http(s) URL" % (cid, conn["baseUrl"]))
            continue
        if u.username or u.password or "@" in u.netloc:
            bad("connection %s: baseUrl carries userinfo - credentials belong in the typed auth "
                "block, which is the only path that gets redaction" % cid)
        if u.query or u.fragment:
            bad("connection %s: baseUrl carries a query string or fragment" % cid)

    declared_integrations = [i["id"] for i in cfg.get("integrations", [])]
    for d in dupes(declared_integrations):
        bad("config: duplicate integration id %r" % d)
    channels = (cfg.get("notifications", {}) or {}).get("channels") or {}

    cards, card_ids = {}, []
    for section in cfg.get("sections", []):
        for card in section.get("cards", []):
            card_ids.append(card["id"])
            cards[card["id"]] = card
    for d in dupes(card_ids):
        bad("config: duplicate card id %r" % d)
    for d in dupes([r["id"] for r in cfg.get("rules", [])]):
        bad("config: duplicate rule id %r" % d)

    for cid, card in cards.items():
        if card.get("integration") and card["integration"] not in declared_integrations:
            bad("card %s: integration %r is not declared in the integrations list"
                % (cid, card["integration"]))
        man = manifests.get(card.get("integration"))
        bindings = card.get("slots") or {}
        for slot, conn in bindings.items():
            if conn not in conns:
                bad("card %s: slot %r bound to undefined connection %r" % (cid, slot, conn))
        if not man:
            continue
        slots = {s["name"]: s for s in man["spec"].get("slots", [])}
        for slot, conn in bindings.items():
            if slot not in slots:
                bad("card %s: binds slot %r which the manifest does not declare" % (cid, slot))
            elif conn in conns and conns[conn]["kind"] != slots[slot]["kind"]:
                bad("card %s: slot %r wants kind %r but connection %r is kind %r"
                    % (cid, slot, slots[slot]["kind"], conn, conns[conn]["kind"]))
        for name, slot in slots.items():
            if slot.get("required", True) and name not in bindings:
                bad("card %s: required slot %r is not bound" % (cid, name))
        ops = {o["id"]: o for o in man["spec"]["operations"]}
        if card.get("operation") not in ops:
            bad("card %s: operation %r not in manifest %s"
                % (cid, card.get("operation"), sorted(ops)))

    out.extend(check_rules(cfg, cards, manifests, channels))

    return out


# ---------------------------------------------------------------- rule expressions
#
# INTENTIONALLY INCOMPLETE, and it fails closed. This is a lexer, not a parser: it splits the
# expression into string and non-string spans and only looks for calls in the non-string spans,
# so `state("x") == "signal(ghost, y)"` is fine while `signal(cardName, "cpu")` is an error.
# It cannot understand precedence, parentheses in arguments, or aliasing. Spike 3 replaces it
# with a walk over the chosen expression language's AST; until then anything it cannot verify
# strictly is reported rather than waved through.

STRICT_SIGNAL = re.compile(r'^signal\(\s*"([^"\\]+)"\s*,\s*"([^"\\]+)"\s*\)')
STRICT_STATE = re.compile(r'^state\(\s*"([^"\\]+)"\s*\)')
CALL_START = re.compile(r'\b(signal|state)\s*\(')


def code_spans(expr):
    """Yields (start, end) spans of expr that are NOT inside a string literal."""
    spans, i, n, start = [], 0, len(expr), 0
    while i < n:
        ch = expr[i]
        if ch in ('"', "'"):
            spans.append((start, i))
            q, i = ch, i + 1
            while i < n and expr[i] != q:
                i += 2 if expr[i] == "\\" else 1
            i += 1
            start = i
        else:
            i += 1
    spans.append((start, n))
    return [(a, b) for a, b in spans if a < b]


def check_rules(cfg, cards, manifests, channels):
    out = []
    for rule in cfg.get("rules", []):
        expr = rule["when"]
        for a, b in code_spans(expr):
            for m in CALL_START.finditer(expr, a, b):
                frag = expr[m.start():]
                sm, tm = STRICT_SIGNAL.match(frag), STRICT_STATE.match(frag)
                if not sm and not tm:
                    out.append("rule %s: %r does not use literal string arguments; dynamic "
                               "signal/state references are rejected rather than left unchecked"
                               % (rule["id"], frag[:40]))
                    continue
                cid = (sm or tm).group(1)
                card = cards.get(cid)
                if card is None:
                    out.append("rule %s: references unknown card %r" % (rule["id"], cid))
                    continue
                if tm:
                    continue
                man = manifests.get(card.get("integration"))
                if man is None:
                    out.append("rule %s: card %r uses integration %r which has no manifest "
                               "available; the example set must be complete"
                               % (rule["id"], cid, card.get("integration")))
                    continue
                op = next((o for o in man["spec"]["operations"]
                           if o["id"] == card.get("operation")), None)
                declared = {sig["name"] for sig in (op or {}).get("signals", [])}
                if sm.group(2) not in declared:
                    out.append("rule %s: signal %r is not declared by %r's operation %r "
                               "(declares %s)" % (rule["id"], sm.group(2), cid,
                                                  card.get("operation"), sorted(declared)))
        for ch in rule.get("notify", []):
            if ch not in channels:
                out.append("rule %s: notifies undefined channel %r" % (rule["id"], ch))
    return out


def cardstate_issues(label, doc):
    """Invariants the JSON Schema cannot express because they compare two fields."""
    out = []
    ex = doc.get("execution", {})
    open_until, next_run = ex.get("circuitOpenUntil"), ex.get("nextRunAt")
    if open_until:
        if not next_run:
            out.append("%s: circuitOpenUntil without nextRunAt" % label)
        elif next_run < open_until:
            out.append("%s: nextRunAt %s is before circuitOpenUntil %s; the probe may be later "
                       "than the earliest permissible time (jitter, scheduling pressure) but "
                       "never earlier" % (label, next_run, open_until))
    return out


def _glob_match(pattern, path):
    """`*` matches within one segment and never crosses `/`. No `**` in v1."""
    pp, sp = pattern.split("/"), path.split("/")
    if len(pp) != len(sp):
        return False
    for a, b in zip(pp, sp):
        if a == b:
            continue
        if "*" not in a:
            return False
        rx = "^" + "[^/]*".join(re.escape(x) for x in a.split("*")) + "$"
        if not re.match(rx, b):
            return False
    return True


def _asset_nodes(node):
    if isinstance(node, dict):
        if "asset" in node and isinstance(node["asset"], dict):
            yield node["asset"]
        for v in node.values():
            yield from _asset_nodes(v)
    elif isinstance(node, list):
        for v in node:
            yield from _asset_nodes(v)
