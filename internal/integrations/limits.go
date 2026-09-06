// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

// limitBound is one field's schema-defined default and core maximum - schemas/plugin-manifest.v1
// #/$defs/limits, which the lock schema $refs so manifest and approval always speak about the
// same fields with the same bounds. Kept in sync with that file by
// TestLimitBoundsMatchManifestSchema, which reads schemas/plugin-manifest.v1.schema.json
// directly rather than trusting this table not to drift.
type limitBound struct{ def, max int }

// limitBounds is keyed by the field's YAML/JSON name across manifest, lock and this package's
// own Limits/EffectiveLimits struct tags.
var limitBounds = map[string]limitBound{
	"memoryMB":      {def: 64, max: 256},
	"timeoutMs":     {def: 3000, max: 30000},
	"outputKB":      {def: 64, max: 256},
	"httpRequests":  {def: 8, max: 32},
	"responseMB":    {def: 4, max: 16},
	"cacheEntries":  {def: 64, max: 512},
	"inputMB":       {def: 4, max: 16},
	"jsonDepth":     {def: 32, max: 64},
	"jsonNodes":     {def: 200000, max: 1000000},
	"exprNodes":     {def: 512, max: 4096},
	"iterations":    {def: 20000, max: 100000},
	"requestBodyKB": {def: 64, max: 1024},
	"hostCalls":     {def: 256, max: 2048},
	"cacheBytesKB":  {def: 256, max: 4096},
}

// limitFieldOrder is limitBounds' keys in the lock schema's own required-field order, so
// output (diagnostics, EffectiveLimits construction) is stable rather than map-iteration order.
var limitFieldOrder = []string{
	"cacheBytesKB", "cacheEntries", "exprNodes", "hostCalls", "httpRequests", "inputMB",
	"iterations", "jsonDepth", "jsonNodes", "memoryMB", "outputKB", "requestBodyKB",
	"responseMB", "timeoutMs",
}

// Limits are a manifest's requested resource ceilings. Every field is a pointer: nil means "not
// set, use the documented default". cacheEntries alone has schema minimum 0, so it is the one
// field where an explicit 0 is a real, distinct value from "unset" - a plain int could not carry
// that distinction, which is why every field here is a pointer rather than only that one.
type Limits struct {
	MemoryMB      *int `yaml:"memoryMB,omitempty" json:"memoryMB,omitempty"`
	TimeoutMs     *int `yaml:"timeoutMs,omitempty" json:"timeoutMs,omitempty"`
	OutputKB      *int `yaml:"outputKB,omitempty" json:"outputKB,omitempty"`
	HTTPRequests  *int `yaml:"httpRequests,omitempty" json:"httpRequests,omitempty"`
	ResponseMB    *int `yaml:"responseMB,omitempty" json:"responseMB,omitempty"`
	CacheEntries  *int `yaml:"cacheEntries,omitempty" json:"cacheEntries,omitempty"`
	InputMB       *int `yaml:"inputMB,omitempty" json:"inputMB,omitempty"`
	JSONDepth     *int `yaml:"jsonDepth,omitempty" json:"jsonDepth,omitempty"`
	JSONNodes     *int `yaml:"jsonNodes,omitempty" json:"jsonNodes,omitempty"`
	ExprNodes     *int `yaml:"exprNodes,omitempty" json:"exprNodes,omitempty"`
	Iterations    *int `yaml:"iterations,omitempty" json:"iterations,omitempty"`
	RequestBodyKB *int `yaml:"requestBodyKB,omitempty" json:"requestBodyKB,omitempty"`
	HostCalls     *int `yaml:"hostCalls,omitempty" json:"hostCalls,omitempty"`
	CacheBytesKB  *int `yaml:"cacheBytesKB,omitempty" json:"cacheBytesKB,omitempty"`
}

// field returns the requested value for name (a limitFieldOrder entry) or false if unset.
func (l Limits) field(name string) (int, bool) {
	var p *int
	switch name {
	case "memoryMB":
		p = l.MemoryMB
	case "timeoutMs":
		p = l.TimeoutMs
	case "outputKB":
		p = l.OutputKB
	case "httpRequests":
		p = l.HTTPRequests
	case "responseMB":
		p = l.ResponseMB
	case "cacheEntries":
		p = l.CacheEntries
	case "inputMB":
		p = l.InputMB
	case "jsonDepth":
		p = l.JSONDepth
	case "jsonNodes":
		p = l.JSONNodes
	case "exprNodes":
		p = l.ExprNodes
	case "iterations":
		p = l.Iterations
	case "requestBodyKB":
		p = l.RequestBodyKB
	case "hostCalls":
		p = l.HostCalls
	case "cacheBytesKB":
		p = l.CacheBytesKB
	default:
		return 0, false
	}
	if p == nil {
		return 0, false
	}
	return *p, true
}

// requested returns one side of the effective(k) formula in isolation, clamped to the core
// maximum: the given Limits' own value if set, else the documented default. Used both for a
// not-yet-approved manifest's own request (there is no "approved" side to fold in yet) and, via
// effectiveLimit, for each side of an actual reconciliation.
func requested(name string, m Limits) int {
	b := limitBounds[name]
	v, ok := m.field(name)
	if !ok {
		v = b.def
	}
	if v > b.max {
		v = b.max
	}
	return v
}

// isZero reports whether l carries no explicit value at all - "no override", as distinct from a
// Limits that explicitly sets every field to its own default. Approve uses this to decide
// whether an entry needs a `limits:` field on the lock at all.
func (l Limits) isZero() bool {
	return l.MemoryMB == nil && l.TimeoutMs == nil && l.OutputKB == nil && l.HTTPRequests == nil &&
		l.ResponseMB == nil && l.CacheEntries == nil && l.InputMB == nil && l.JSONDepth == nil &&
		l.JSONNodes == nil && l.ExprNodes == nil && l.Iterations == nil && l.RequestBodyKB == nil &&
		l.HostCalls == nil && l.CacheBytesKB == nil
}

// effectiveLimit is docs/01-architecture.md section 6's formula in full:
// effective(k) = min(core maximum(k), manifest(k) ?? default(k), approved(k) ?? default(k)).
// approved is genuinely independent of manifest - an integration requesting a limit above the
// documented default is only actually granted it if the lock's own (optional) `limits:` field
// explicitly says so; an integration approved with no such override stays at the default no
// matter what its manifest asks for. This mirrors internal/contracts/semantic.go's
// (*checker).effectiveLimit exactly, which is what examples/veduta.lock.yaml is already checked
// against - immich's lock entry carries an explicit `limits: {httpRequests:2, timeoutMs:5000}`
// for exactly this reason, while glances' `limits: {timeoutMs:4000}` was ADDED as part of D2b
// after this reconciliation was first implemented here and caught a lock entry that raised
// effectiveLimits.timeoutMs to 4000 with no override on record to justify it.
func effectiveLimit(name string, manifest, approved Limits) int {
	m := requested(name, manifest)
	a := requested(name, approved)
	if a < m {
		return a
	}
	return m
}

// EffectiveLimits is the lock's required, fully-populated record of what was actually enforced
// at approval time - schemas/integration-lock.v1's effectiveLimits, every one of whose fourteen
// fields is required (a partial map would leave some limits silently governed by whatever the
// core default happens to be at the time, exactly what recording this snapshot exists to avoid).
type EffectiveLimits struct {
	MemoryMB      int `yaml:"memoryMB" json:"memoryMB"`
	TimeoutMs     int `yaml:"timeoutMs" json:"timeoutMs"`
	OutputKB      int `yaml:"outputKB" json:"outputKB"`
	HTTPRequests  int `yaml:"httpRequests" json:"httpRequests"`
	ResponseMB    int `yaml:"responseMB" json:"responseMB"`
	CacheEntries  int `yaml:"cacheEntries" json:"cacheEntries"`
	InputMB       int `yaml:"inputMB" json:"inputMB"`
	JSONDepth     int `yaml:"jsonDepth" json:"jsonDepth"`
	JSONNodes     int `yaml:"jsonNodes" json:"jsonNodes"`
	ExprNodes     int `yaml:"exprNodes" json:"exprNodes"`
	Iterations    int `yaml:"iterations" json:"iterations"`
	RequestBodyKB int `yaml:"requestBodyKB" json:"requestBodyKB"`
	HostCalls     int `yaml:"hostCalls" json:"hostCalls"`
	CacheBytesKB  int `yaml:"cacheBytesKB" json:"cacheBytesKB"`
}

// field returns e's value for a limitFieldOrder entry - the read side of the same dispatch
// Limits.field provides, used by the diff to compare against a manifest's requested value.
func (e EffectiveLimits) field(name string) int {
	switch name {
	case "memoryMB":
		return e.MemoryMB
	case "timeoutMs":
		return e.TimeoutMs
	case "outputKB":
		return e.OutputKB
	case "httpRequests":
		return e.HTTPRequests
	case "responseMB":
		return e.ResponseMB
	case "cacheEntries":
		return e.CacheEntries
	case "inputMB":
		return e.InputMB
	case "jsonDepth":
		return e.JSONDepth
	case "jsonNodes":
		return e.JSONNodes
	case "exprNodes":
		return e.ExprNodes
	case "iterations":
		return e.Iterations
	case "requestBodyKB":
		return e.RequestBodyKB
	case "hostCalls":
		return e.HostCalls
	case "cacheBytesKB":
		return e.CacheBytesKB
	default:
		return 0
	}
}

// setField writes name into e - the construction side, kept next to field() so the two can
// never drift onto different field lists.
func (e *EffectiveLimits) setField(name string, v int) {
	switch name {
	case "memoryMB":
		e.MemoryMB = v
	case "timeoutMs":
		e.TimeoutMs = v
	case "outputKB":
		e.OutputKB = v
	case "httpRequests":
		e.HTTPRequests = v
	case "responseMB":
		e.ResponseMB = v
	case "cacheEntries":
		e.CacheEntries = v
	case "inputMB":
		e.InputMB = v
	case "jsonDepth":
		e.JSONDepth = v
	case "jsonNodes":
		e.JSONNodes = v
	case "exprNodes":
		e.ExprNodes = v
	case "iterations":
		e.Iterations = v
	case "requestBodyKB":
		e.RequestBodyKB = v
	case "hostCalls":
		e.HostCalls = v
	case "cacheBytesKB":
		e.CacheBytesKB = v
	}
}

// ReconcileAtApproval computes effectiveLimits at the moment a manifest is approved: every field
// via effectiveLimit(manifest, approved). approved is Grants.Limits - the admin's own explicit
// override, Limits{} (zero value) if none was given, which folds every field to its default on
// that side of the formula.
func ReconcileAtApproval(manifest, approved Limits) EffectiveLimits {
	var out EffectiveLimits
	for _, name := range limitFieldOrder {
		out.setField(name, effectiveLimit(name, manifest, approved))
	}
	return out
}
