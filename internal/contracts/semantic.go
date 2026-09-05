// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CoreDefaultBodyKB is the request-body ceiling when neither a route nor a manifest narrows it.
// There is no "unlimited".
const CoreDefaultBodyKB = 64

// Documents is everything the cross-document checks need. Values are decoded documents
// (map[string]any) rather than typed structs so the checks can run before the configuration
// types exist, and so a negative fixture can express any malformation at all.
type Documents struct {
	Manifests map[string]any // integration id -> manifest
	Lock      map[string]any
	Config    map[string]any
	// Limits is the shared limits definition from the manifest schema; defaults and maxima are
	// read from it rather than duplicated here.
	Limits map[string]any
	// Digest computes a canonical manifest digest (see internal/canonical).
	Digest func(any) (string, error)
}

type checker struct {
	Documents
	issues   []string
	defaults map[string]float64
	maxima   map[string]float64
}

func (c *checker) badf(format string, a ...any) {
	c.issues = append(c.issues, fmt.Sprintf(format, a...))
}

// Issues returns every cross-document problem found. An empty slice means clean.
func Issues(d Documents) []string {
	c := &checker{Documents: d, defaults: map[string]float64{}, maxima: map[string]float64{}}
	for name, raw := range mapOf(d.Limits, "properties") {
		spec := asMap(raw)
		if v, ok := numberOf(spec, "default"); ok {
			c.defaults[name] = v
		}
		if v, ok := numberOf(spec, "maximum"); ok {
			c.maxima[name] = v
		}
	}
	c.checkManifests()
	c.checkLock()
	c.checkConfig()
	sort.Strings(c.issues)
	return c.issues
}

// ---------------------------------------------------------------- manifests

func (c *checker) checkManifests() {
	for _, iid := range sortedKeys(c.Manifests) {
		man := asMap(c.Manifests[iid])
		c.scanValues("manifest/"+iid, man)
		meta := mapOf(man, "metadata")
		if v, _ := meta["version"].(string); !ValidSemver(v) {
			c.badf("manifest/%s: version %q is not valid SemVer 2.0.0", iid, v)
		}
		spec := mapOf(man, "spec")
		slots := map[string]map[string]any{}
		for _, s := range sliceOf(spec, "slots") {
			sm := asMap(s)
			slots[strOf(sm, "name")] = sm
		}
		c.checkDuplicates(iid, "slot name", sliceOf(spec, "slots"), "name")
		c.checkDuplicates(iid, "operation id", sliceOf(spec, "operations"), "id")

		caps := map[string]bool{}
		for _, v := range sliceOf(spec, "capabilities") {
			if s, ok := v.(string); ok {
				caps[s] = true
			}
		}
		bodyKB := c.bodyCeiling(spec)
		for name, v := range mapOf(spec, "limits") {
			if n, ok := toNumber(v); ok {
				if max, has := c.maxima[name]; has && n > max {
					c.badf("%s: limit %s=%v exceeds the core maximum %v", iid, name, n, max)
				}
			}
		}
		for _, opRaw := range sliceOf(spec, "operations") {
			c.checkOperation(iid, asMap(opRaw), slots, caps, bodyKB)
		}
	}
}

func (c *checker) checkOperation(iid string, op map[string]any, slots map[string]map[string]any,
	caps map[string]bool, bodyKB float64) {
	oid := iid + "/" + strOf(op, "id")
	routes := sliceOf(op, "routes")

	declared := map[string]bool{}
	for _, s := range sliceOf(op, "signals") {
		declared[strOf(asMap(s), "name")] = true
	}
	c.checkDuplicates(oid, "signal declaration", sliceOf(op, "signals"), "name")

	seen := map[string]bool{}
	hasData, hasAsset := false, false
	for _, rRaw := range routes {
		r := asMap(rRaw)
		id := c.routeIdentity(r, bodyKB)
		if seen[id] {
			c.badf("%s: duplicate route %s", oid, id)
		}
		seen[id] = true
		if use := routeUse(r); use == "asset" {
			hasAsset = true
		} else {
			hasData = true
		}
		if slot := strOf(r, "slot"); slots[slot] == nil {
			c.badf("%s: route references undeclared slot %q", oid, slot)
		}
		if v, ok := numberOf(r, "maxBodyKB"); ok && v > bodyKB {
			c.badf("%s: route maxBodyKB widens the manifest requestBodyKB", oid)
		}
		if ct := strOf(r, "contentType"); ct != "" {
			if _, err := NormaliseMediaType(ct); err != nil {
				c.badf("%s: invalid media type %q", oid, ct)
			}
		}
	}
	// A use=data route needs http; a use=asset route needs assets. An asset-only integration
	// must not be forced to request a capability it never uses.
	if hasData && !caps["http"] {
		c.badf("%s: has data routes but does not request the http capability", oid)
	}
	if hasAsset && !caps["assets"] {
		c.badf("%s: has asset routes but does not request the assets capability", oid)
	}

	for _, stepRaw := range sliceOf(op, "pipeline") {
		c.checkPipelineStep(oid, asMap(stepRaw), slots, routes, bodyKB)
	}

	for _, name := range sortedKeys(mapOf(mapOf(op, "output"), "signals")) {
		if !declared[name] {
			c.badf("%s: emits signal %q not declared by THIS operation", oid, name)
		}
	}
	for _, node := range assetNodes(op["output"]) {
		slot := strOf(node, "slot")
		found := false
		for _, rRaw := range routes {
			r := asMap(rRaw)
			if strOf(r, "slot") == slot && routeUse(r) == "asset" {
				found = true
			}
		}
		if !found {
			c.badf("%s: asset node for slot %q has no use=asset route", oid, slot)
		}
	}
}

// checkPipelineStep decides every constraint that is statically decidable. Anything dynamic -
// body size, expression-valued paths - is deferred to the broker at runtime by design.
func (c *checker) checkPipelineStep(oid string, step map[string]any,
	slots map[string]map[string]any, routes []any, bodyKB float64) {
	req := mapOf(step, "request")
	slot := strOf(req, "slot")
	if slots[slot] == nil {
		c.badf("%s: pipeline references undeclared slot %q", oid, slot)
		return
	}
	pathRaw, ok := req["path"].(string)
	if !ok {
		c.badf("%s: pipeline path is an expression; v1 requires literal paths so route coverage "+
			"is decidable at load time", oid)
		return
	}
	method := "GET"
	if m, ok := req["method"].(string); ok {
		method = m
	}

	var candidates []map[string]any
	for _, rRaw := range routes {
		r := asMap(rRaw)
		if strOf(r, "slot") == slot && strOf(r, "method") == method &&
			routeUse(r) == "data" && GlobMatch(strOf(r, "path"), pathRaw) {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		c.badf("%s: pipeline request %s %s is not covered by any declared route", oid, method, pathRaw)
		return
	}

	sent := sortedKeys(mapOf(req, "query"))
	body := mapOf(req, "body")
	bodyKind := ""
	if _, ok := body["json"]; ok {
		bodyKind = "json"
	} else if _, ok := body["form"]; ok {
		bodyKind = "form"
	}

	var reasons []string
	for _, r := range candidates {
		if keys, declared := r["queryKeys"]; declared {
			allowed := map[string]bool{}
			for _, k := range asSlice(keys) {
				if s, ok := k.(string); ok {
					allowed[s] = true
				}
			}
			var extra []string
			for _, k := range sent {
				if !allowed[k] {
					extra = append(extra, k)
				}
			}
			if len(extra) > 0 {
				reasons = append(reasons, fmt.Sprintf("sends query keys %v outside the route allowlist %v",
					extra, sortedKeys(allowed)))
				continue
			}
		}
		if bodyKind != "" {
			ct := strOf(r, "contentType")
			if ct == "" {
				reasons = append(reasons, fmt.Sprintf("sends a %s body but the route declares no contentType", bodyKind))
				continue
			}
			n, err := NormaliseMediaType(ct)
			if err != nil {
				c.badf("%s: invalid media type %q", oid, ct)
				continue
			}
			base, _, _ := strings.Cut(n, ";")
			wantJSON := base == "application/json" || strings.HasSuffix(base, "+json")
			wantForm := base == "application/x-www-form-urlencoded"
			if bodyKind == "json" && !wantJSON {
				reasons = append(reasons, fmt.Sprintf("sends a json body but the route declares %q", ct))
				continue
			}
			if bodyKind == "form" && !wantForm {
				reasons = append(reasons, fmt.Sprintf("sends a form body but the route declares %q", ct))
				continue
			}
		}
		return // satisfied
	}
	c.badf("%s: pipeline request %s %s matches a route path but violates its constraints: %s",
		oid, method, pathRaw, strings.Join(reasons, "; "))
}

// ---------------------------------------------------------------- lock

func (c *checker) checkLock() {
	c.scanValues("lock", c.Lock)
	entries := mapOf(c.Lock, "integrations")
	for _, iid := range sortedKeys(entries) {
		entry := asMap(entries[iid])
		manRaw, ok := c.Manifests[iid]
		if !ok {
			c.badf("lock/%s: no manifest for this integration - the example set must be complete, "+
				"a skipped entry is not a passing check", iid)
			continue
		}
		man := asMap(manRaw)
		spec := mapOf(man, "spec")
		bodyKB := c.bodyCeiling(spec)

		requested := map[string]bool{}
		for _, opRaw := range sliceOf(spec, "operations") {
			for _, rRaw := range sliceOf(asMap(opRaw), "routes") {
				requested[c.routeIdentity(asMap(rRaw), bodyKB)] = true
			}
		}
		for _, rRaw := range sliceOf(entry, "routes") {
			if id := c.routeIdentity(asMap(rRaw), bodyKB); !requested[id] {
				c.badf("lock/%s: approves a route the manifest never requested (full identity): %s", iid, id)
			}
		}
		manCaps := map[string]bool{}
		for _, v := range sliceOf(spec, "capabilities") {
			if s, ok := v.(string); ok {
				manCaps[s] = true
			}
		}
		for _, v := range sliceOf(entry, "capabilities") {
			if s, ok := v.(string); ok && !manCaps[s] {
				c.badf("lock/%s: approves capability %q the manifest never requested", iid, s)
			}
		}
		if rt := strOf(entry, "runtime"); rt != "" && rt != strOf(spec, "runtime") {
			c.badf("lock/%s: records runtime %q but the manifest declares %q", iid, rt, strOf(spec, "runtime"))
		}

		approved := mapOf(entry, "limits")
		manLimits := mapOf(spec, "limits")
		for _, k := range sortedKeys(approved) {
			v, ok := toNumber(approved[k])
			if !ok {
				continue
			}
			// An omitted manifest limit means its documented DEFAULT, never "anything".
			req, has := numberOf(manLimits, k)
			if !has {
				req, has = c.defaults[k], true
			}
			if has && v > req {
				c.badf("lock/%s: limit %s=%v exceeds the manifest's effective request %v "+
					"(manifest value or documented default)", iid, k, v, req)
			}
		}

		eff, present := entry["effectiveLimits"]
		if !present || eff == nil {
			c.badf("lock/%s: effectiveLimits is missing; without it a change to a core default "+
				"silently moves authority instead of forcing re-approval", iid)
		} else {
			effMap := asMap(eff)
			var missing, unknown []string
			for _, k := range sortedKeys(c.defaults) {
				if _, ok := effMap[k]; !ok {
					missing = append(missing, k)
				}
			}
			for _, k := range sortedKeys(effMap) {
				if _, ok := c.defaults[k]; !ok {
					unknown = append(unknown, k)
				}
			}
			if len(missing) > 0 {
				c.badf("lock/%s: effectiveLimits is incomplete, missing %v", iid, missing)
			}
			if len(unknown) > 0 {
				c.badf("lock/%s: effectiveLimits has unknown keys %v", iid, unknown)
			}
			for _, k := range sortedKeys(effMap) {
				v, ok := toNumber(effMap[k])
				if !ok {
					continue
				}
				want := c.effectiveLimit(k, manLimits, approved)
				if v != want {
					c.badf("lock/%s: effectiveLimits.%s=%v but min(core, manifest, approved)=%v",
						iid, k, v, want)
				}
			}
		}

		if c.Digest != nil {
			computed, err := c.Digest(man)
			if err != nil {
				c.badf("lock/%s: manifest is not canonicalisable: %v", iid, err)
			} else if got := strOf(entry, "manifestSha256"); got != computed {
				c.badf("lock/%s: manifestSha256 %s… does not match the canonical digest %s…",
					iid, truncate(got, 12), truncate(computed, 12))
			}
		}
	}
}

// effectiveLimit is min(core maximum, manifest value-or-default, approved value-or-default).
// An omitted limit means its documented default, never "anything" - and never, as an earlier
// draft of this function had it, a floor that silently overrode a higher explicit request.
func (c *checker) effectiveLimit(k string, manLimits, approved map[string]any) float64 {
	manifest, ok := numberOf(manLimits, k)
	if !ok {
		manifest = c.defaults[k]
	}
	approvedVal, ok := numberOf(approved, k)
	if !ok {
		approvedVal = c.defaults[k]
	}
	want := min(manifest, approvedVal)
	if max, ok := c.maxima[k]; ok {
		want = min(want, max)
	}
	return want
}

// ---------------------------------------------------------------- configuration

func (c *checker) checkConfig() {
	c.scanValues("config", c.Config)
	conns := mapOf(c.Config, "connections")
	for _, cid := range sortedKeys(conns) {
		conn := asMap(conns[cid])
		if strOf(conn, "kind") != "http" {
			continue
		}
		raw := strOf(conn, "baseUrl")
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || !validPort(u) {
			c.badf("connection %s: baseUrl %q is not a usable http(s) URL", cid, raw)
			continue
		}
		if u.User != nil || strings.Contains(u.Host, "@") {
			c.badf("connection %s: baseUrl carries userinfo - credentials belong in the typed auth "+
				"block, which is the only path that gets redaction", cid)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			c.badf("connection %s: baseUrl carries a query string or fragment", cid)
		}
	}

	declared := map[string]bool{}
	var integrationIDs []any
	for _, iRaw := range sliceOf(c.Config, "integrations") {
		i := asMap(iRaw)
		integrationIDs = append(integrationIDs, i)
		declared[strOf(i, "id")] = true
	}
	c.checkDuplicates("config", "integration id", integrationIDs, "id")

	channels := mapOf(mapOf(c.Config, "notifications"), "channels")
	cards := map[string]map[string]any{}
	var cardList []any
	for _, secRaw := range sliceOf(c.Config, "sections") {
		for _, cardRaw := range sliceOf(asMap(secRaw), "cards") {
			card := asMap(cardRaw)
			cardList = append(cardList, card)
			cards[strOf(card, "id")] = card
		}
	}
	c.checkDuplicates("config", "card id", cardList, "id")
	c.checkDuplicates("config", "rule id", sliceOf(c.Config, "rules"), "id")

	for _, cid := range sortedKeys(cards) {
		card := cards[cid]
		integration := strOf(card, "integration")
		if integration != "" && !declared[integration] {
			c.badf("card %s: integration %q is not declared in the integrations list", cid, integration)
		}
		bindings := mapOf(card, "slots")
		for _, slot := range sortedKeys(bindings) {
			conn := strOf(bindings, slot)
			if _, ok := conns[conn]; !ok {
				c.badf("card %s: slot %q bound to undefined connection %q", cid, slot, conn)
			}
		}
		manRaw, ok := c.Manifests[integration]
		if !ok {
			continue
		}
		spec := mapOf(asMap(manRaw), "spec")
		slots := map[string]map[string]any{}
		for _, s := range sliceOf(spec, "slots") {
			sm := asMap(s)
			slots[strOf(sm, "name")] = sm
		}
		for _, slot := range sortedKeys(bindings) {
			conn := strOf(bindings, slot)
			sm, ok := slots[slot]
			if !ok {
				c.badf("card %s: binds slot %q which the manifest does not declare", cid, slot)
				continue
			}
			if cm, ok := conns[conn]; ok {
				if kind := strOf(asMap(cm), "kind"); kind != strOf(sm, "kind") {
					c.badf("card %s: slot %q wants kind %q but connection %q is kind %q",
						cid, slot, strOf(sm, "kind"), conn, kind)
				}
			}
		}
		for _, name := range sortedKeys(slots) {
			required := true
			if v, ok := slots[name]["required"].(bool); ok {
				required = v
			}
			if _, bound := bindings[name]; required && !bound {
				c.badf("card %s: required slot %q is not bound", cid, name)
			}
		}
		var ops []string
		found := false
		for _, opRaw := range sliceOf(spec, "operations") {
			id := strOf(asMap(opRaw), "id")
			ops = append(ops, id)
			if id == strOf(card, "operation") {
				found = true
			}
		}
		if !found {
			c.badf("card %s: operation %q not in manifest %v", cid, strOf(card, "operation"), ops)
		}
	}

	c.checkRules(cards, channels)
}

// ---------------------------------------------------------------- rules
//
// INTENTIONALLY INCOMPLETE, and it fails closed. This is a lexer, not a parser: it splits the
// expression into string and non-string spans and only looks for calls in the non-string spans,
// so state("x") == "signal(ghost, y)" is fine while signal(cardName, "cpu") is an error. It
// cannot understand precedence or aliasing. Spike 3 replaces it with a walk over the chosen
// expression language's AST; until then anything it cannot verify strictly is reported.

var (
	callStart    = regexp.MustCompile(`\b(signal|state)\s*\(`)
	strictSignal = regexp.MustCompile(`^signal\(\s*"([^"\\]+)"\s*,\s*"([^"\\]+)"\s*\)`)
	strictState  = regexp.MustCompile(`^state\(\s*"([^"\\]+)"\s*\)`)
)

// CodeSpans returns the byte ranges of expr that are NOT inside a string literal.
func CodeSpans(expr string) [][2]int {
	var spans [][2]int
	start, i := 0, 0
	for i < len(expr) {
		ch := expr[i]
		if ch == '"' || ch == '\'' {
			spans = append(spans, [2]int{start, i})
			q := ch
			i++
			for i < len(expr) && expr[i] != q {
				if expr[i] == '\\' {
					i += 2
				} else {
					i++
				}
			}
			i++
			start = i
		} else {
			i++
		}
	}
	spans = append(spans, [2]int{start, len(expr)})
	out := spans[:0]
	for _, s := range spans {
		if s[0] < s[1] && s[1] <= len(expr) {
			out = append(out, s)
		}
	}
	return out
}

func (c *checker) checkRules(cards map[string]map[string]any, channels map[string]any) {
	for _, ruleRaw := range sliceOf(c.Config, "rules") {
		rule := asMap(ruleRaw)
		id := strOf(rule, "id")
		expr := strOf(rule, "when")
		for _, span := range CodeSpans(expr) {
			segment := expr[span[0]:span[1]]
			for _, loc := range callStart.FindAllStringIndex(segment, -1) {
				frag := expr[span[0]+loc[0]:]
				sm := strictSignal.FindStringSubmatch(frag)
				tm := strictState.FindStringSubmatch(frag)
				if sm == nil && tm == nil {
					c.badf("rule %s: %q does not use literal string arguments; dynamic signal/state "+
						"references are rejected rather than left unchecked", id, truncate(frag, 40))
					continue
				}
				cardID := sm
				if cardID == nil {
					cardID = tm
				}
				card, ok := cards[cardID[1]]
				if !ok {
					c.badf("rule %s: references unknown card %q", id, cardID[1])
					continue
				}
				if sm == nil {
					continue
				}
				manRaw, ok := c.Manifests[strOf(card, "integration")]
				if !ok {
					c.badf("rule %s: card %q uses integration %q which has no manifest available; "+
						"the example set must be complete", id, cardID[1], strOf(card, "integration"))
					continue
				}
				var declared []string
				for _, opRaw := range sliceOf(mapOf(asMap(manRaw), "spec"), "operations") {
					op := asMap(opRaw)
					if strOf(op, "id") != strOf(card, "operation") {
						continue
					}
					for _, sRaw := range sliceOf(op, "signals") {
						declared = append(declared, strOf(asMap(sRaw), "name"))
					}
				}
				if !contains(declared, sm[2]) {
					c.badf("rule %s: signal %q is not declared by %q's operation %q (declares %v)",
						id, sm[2], cardID[1], strOf(card, "operation"), declared)
				}
			}
		}
		for _, ch := range sliceOf(rule, "notify") {
			if s, ok := ch.(string); ok {
				if _, exists := channels[s]; !exists {
					c.badf("rule %s: notifies undefined channel %q", id, s)
				}
			}
		}
	}
}

// CardStateIssues checks invariants that compare two fields, which JSON Schema cannot express.
func CardStateIssues(label string, doc map[string]any) []string {
	var out []string
	ex := mapOf(doc, "execution")
	openUntil, nextRun := strOf(ex, "circuitOpenUntil"), strOf(ex, "nextRunAt")
	if openUntil == "" {
		return nil
	}
	if nextRun == "" {
		return append(out, label+": circuitOpenUntil without nextRunAt")
	}
	a, err1 := time.Parse(time.RFC3339, openUntil)
	b, err2 := time.Parse(time.RFC3339, nextRun)
	if err1 == nil && err2 == nil && b.Before(a) {
		out = append(out, fmt.Sprintf("%s: nextRunAt %s is before circuitOpenUntil %s; the probe may "+
			"be later than the earliest permissible time (jitter, scheduling pressure) but never earlier",
			label, nextRun, openUntil))
	}
	return out
}

// ---------------------------------------------------------------- shared pieces

// GlobMatch implements the v1 route pattern: * matches within one path segment and never
// crosses "/". There is no ** in v1.
func GlobMatch(pattern, path string) bool {
	pp, sp := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(pp) != len(sp) {
		return false
	}
	for i := range pp {
		if pp[i] == sp[i] {
			continue
		}
		if !strings.Contains(pp[i], "*") {
			return false
		}
		parts := strings.Split(pp[i], "*")
		for j := range parts {
			parts[j] = regexp.QuoteMeta(parts[j])
		}
		re, err := regexp.Compile("^" + strings.Join(parts, "[^/]*") + "$")
		if err != nil || !re.MatchString(sp[i]) {
			return false
		}
	}
	return true
}

// routeIdentity is the FULL authority tuple. Comparing by method and path alone would let a
// lock entry silently drop a constraint the manifest declared.
func (c *checker) routeIdentity(r map[string]any, bodyKB float64) string {
	qk := "nil" // nil means "any key except connection-owned", which is WIDER than any list
	if raw, declared := r["queryKeys"]; declared {
		var keys []string
		for _, k := range asSlice(raw) {
			if s, ok := k.(string); ok {
				keys = append(keys, s)
			}
		}
		sort.Strings(keys)
		keys = dedupe(keys)
		qk = "[" + strings.Join(keys, ",") + "]"
	}
	ct := "nil"
	if raw := strOf(r, "contentType"); raw != "" {
		n, err := NormaliseMediaType(raw)
		if err != nil {
			ct = "<invalid>" + raw
		} else {
			ct = n
		}
	}
	body := bodyKB
	if v, ok := numberOf(r, "maxBodyKB"); ok {
		body = v
	}
	return fmt.Sprintf("(%s %s %s use=%s query=%s ct=%s body=%gKB)",
		strOf(r, "slot"), strOf(r, "method"), strOf(r, "path"), routeUse(r), qk, ct, body)
}

func (c *checker) bodyCeiling(spec map[string]any) float64 {
	if v, ok := numberOf(mapOf(spec, "limits"), "requestBodyKB"); ok {
		return v
	}
	if v, ok := c.defaults["requestBodyKB"]; ok {
		return v
	}
	return CoreDefaultBodyKB
}

func (c *checker) checkDuplicates(scope, what string, items []any, key string) {
	seen := map[string]bool{}
	for _, raw := range items {
		v := strOf(asMap(raw), key)
		if v == "" {
			continue
		}
		if seen[v] {
			c.badf("%s: duplicate %s %q", scope, what, v)
		}
		seen[v] = true
	}
}

// scanValues enforces that timestamps and URLs are real values, not merely pattern-shaped
// strings: a pattern accepts 2026-99-99T99:99:99Z, and no readable regex validates ports or
// bracketed IPv6.
func (c *checker) scanValues(label string, doc any) {
	timeKeys := map[string]bool{"generatedAt": true, "expiresAt": true, "staleSince": true,
		"nextRunAt": true, "circuitOpenUntil": true, "approvedAt": true, "since": true,
		"timestamp": true, "at": true}
	urlKeys := map[string]bool{"url": true, "link": true, "homepage": true}
	var walk func(node any, path string, key string)
	walk = func(node any, path, key string) {
		switch t := node.(type) {
		case map[string]any:
			for _, k := range sortedKeys(t) {
				walk(t[k], path+"."+k, k)
			}
		case []any:
			for i, v := range t {
				walk(v, fmt.Sprintf("%s[%d]", path, i), key)
			}
		case string:
			if timeKeys[key] {
				if _, err := time.Parse(time.RFC3339, t); err != nil {
					c.badf("%s%s: %q is pattern-shaped but not a real timestamp", label, path, t)
				}
			}
			if urlKeys[key] {
				u, err := url.Parse(t)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || !validPort(u) {
					c.badf("%s%s: %q is not a usable http(s) URL", label, path, t)
				}
			}
		}
	}
	walk(doc, "", "")
}

func validPort(u *url.URL) bool {
	p := u.Port()
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

func assetNodes(node any) []map[string]any {
	var out []map[string]any
	switch t := node.(type) {
	case map[string]any:
		if a, ok := t["asset"].(map[string]any); ok {
			out = append(out, a)
		}
		for _, k := range sortedKeys(t) {
			out = append(out, assetNodes(t[k])...)
		}
	case []any:
		for _, v := range t {
			out = append(out, assetNodes(v)...)
		}
	}
	return out
}

func routeUse(r map[string]any) string {
	if u := strOf(r, "use"); u != "" {
		return u
	}
	return "data"
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func mapOf(m map[string]any, key string) map[string]any { return asMap(m[key]) }
func sliceOf(m map[string]any, key string) []any        { return asSlice(m[key]) }

func strOf(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func numberOf(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	return toNumber(v)
}

func toNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		// jsonschema.UnmarshalJSON decodes numbers as json.Number so large integers survive.
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

func dedupe(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
