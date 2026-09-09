// SPDX-License-Identifier: AGPL-3.0-or-later

// Package app composes configuration, approved integrations, connections, and scheduling.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sort"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/integrations"
	"veduta.dev/veduta/internal/integrations/declarative"
	dockerintegration "veduta.dev/veduta/internal/integrations/docker"
	"veduta.dev/veduta/internal/integrations/httpjson"
	"veduta.dev/veduta/internal/integrations/manifestload"
	wasmrt "veduta.dev/veduta/internal/integrations/wasm"
	"veduta.dev/veduta/internal/rules"
	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

// Generation owns every integration runtime used by one accepted configuration.
// Close it only after the scheduler has cancelled the generation's invocations.
type Generation struct {
	Definitions      []scheduler.Definition
	RuleDefinitions  []rules.Definition
	RuleDeclarations map[string]rules.CardDefinition
	PluginLoads      []PluginLoad
	runtimes         []integrations.Runtime
}

// PluginLoad identifies an external plugin loaded by a generation for audit attribution.
type PluginLoad struct {
	ID, Runtime, Version, Digest string
}

// Close releases runtime caches and waits for any bounded invocation still leaving the old generation.
func (g *Generation) Close(ctx context.Context) error {
	if g == nil {
		return nil
	}
	var errs []error
	for i := len(g.runtimes) - 1; i >= 0; i-- {
		errs = append(errs, g.runtimes[i].Close(ctx))
	}
	g.runtimes = nil
	return errors.Join(errs...)
}

// BuildGeneration resolves every card to an approved runtime instance and scheduler definition.
func BuildGeneration(ctx context.Context, snap *config.Snapshot, generation uint64, configPath string, reg connections.Registry, revisions map[string]string, assets *assettokens.Service, wasmCacheDir string, logger *slog.Logger) (_ *Generation, err error) {
	return BuildGenerationWithAudit(ctx, snap, generation, configPath, reg, revisions, assets, wasmCacheDir, capabilities.NewMemAudit(), logger)
}

// BuildGenerationWithAudit builds a generation and persists broker denials through auditSink.
func BuildGenerationWithAudit(ctx context.Context, snap *config.Snapshot, generation uint64, configPath string, reg connections.Registry, revisions map[string]string, assets *assettokens.Service, wasmCacheDir string, auditSink capabilities.Audit, logger *slog.Logger) (_ *Generation, err error) {
	return BuildGenerationWithAuditAndEvents(ctx, snap, generation, configPath, reg, revisions, assets, wasmCacheDir, auditSink, nil, logger)
}

// BuildGenerationWithAuditAndEvents also connects authorised plugin events to persistence.
func BuildGenerationWithAuditAndEvents(ctx context.Context, snap *config.Snapshot, generation uint64, configPath string, reg connections.Registry, revisions map[string]string, assets *assettokens.Service, wasmCacheDir string, auditSink capabilities.Audit, eventSink func(context.Context, string, capabilities.Event) error, logger *slog.Logger) (_ *Generation, err error) {
	broker := capabilities.NewBrokerWithAssetsAndEvents(reg, capabilities.NewMemCache(), auditSink, logger, assets, eventSink)
	built := &Generation{}
	loaded := make(map[string]struct{})
	declarativeRuntime := declarative.New(broker)
	built.runtimes = append(built.runtimes, declarativeRuntime)
	var wasmRuntime integrations.Runtime
	defer func() {
		if err != nil {
			_ = built.Close(context.Background())
		}
	}()
	configDir := filepath.Dir(configPath)
	lock, err := integrations.ReadLock(filepath.Join(configDir, integrations.LockFileName))
	if err != nil {
		return nil, err
	}
	for _, section := range snap.Config.Sections {
		for _, card := range section.Cards {
			d := scheduler.Definition{ID: card.ID, Source: state.Source{Integration: card.Integration, Operation: card.Operation, Slots: card.Slots}}
			d.Hash, err = storage.DefinitionHash(struct {
				Integration, Operation string
				Slots                  map[string]string
				Params                 map[string]any
			}{card.Integration, card.Operation, card.Slots, card.Params})
			if err != nil {
				return nil, err
			}
			slotRevisions := revisionsFor(card.Slots, revisions)
			slotRevisionJSON, err := json.Marshal(slotRevisions)
			if err != nil {
				return nil, err
			}
			d.SlotRevisions = string(slotRevisionJSON)
			d.Refresh = parseDuration(card.Refresh, time.Minute)
			d.Timeout = 10 * time.Second
			if card.Integration == "docker" {
				d.SignalTypes = map[string]string{"containers.running": "number", "containers.total": "number"}
				declared, exists := snap.IntegrationByID("docker")
				connectionID, bound := card.Slots["server"]
				connection, connected := reg.Get(connectionID)
				dockerRegistry, supported := reg.(connections.DockerRegistry)
				if !exists || declared.Source != "builtin" || !bound || !connected || connection.Kind != connections.KindDocker || !supported || card.Operation != "containers" {
					d.Disabled = state.ReasonConfigError
					built.Definitions = append(built.Definitions, d)
					continue
				}
				d.Source.Runtime = state.RuntimeBuiltin
				d.Source.IntegrationVersion = "1.0.0"
				d.ManifestDigest = "docker-builtin-v1"
				d.Key = definitionKey(d.ManifestDigest, card.Operation, card.Slots, slotRevisions, card.Params)
				dockerConnectionID := connectionID
				d.Run = func(c context.Context) (widgets.Document, error) {
					return dockerintegration.Containers(c, dockerRegistry, dockerConnectionID)
				}
				built.Definitions = append(built.Definitions, d)
				continue
			}
			if card.Integration == "http-json" {
				slot, connID := firstSlot(card.Slots)
				conn, _ := reg.Get(connID)
				policy := capabilities.ConnectionPolicy{}
				if conn != nil && conn.HTTP != nil {
					policy.AllowedPaths = conn.HTTP.AllowedPaths
				}
				hc := httpjson.Card{Path: stringParam(card.Params, "path"), Method: stringParam(card.Params, "method"), Query: stringMapParam(card.Params, "query"), View: card.View}
				m, l, g, e := httpjson.Build(slot, connID, hc, policy)
				if e != nil {
					d.Disabled = state.ReasonConfigError
					built.Definitions = append(built.Definitions, d)
					continue
				}
				g.Ident.SnapshotGen = generation
				g.Ident.CardHash = d.Hash
				g.Ident.SlotRevisions = slotRevisions
				inst, e := declarativeRuntime.Load(ctx, integrations.Installed{Manifest: m, Lock: l})
				if e != nil {
					return nil, e
				}
				d.Source.Runtime = state.RuntimeBuiltin
				d.Source.IntegrationVersion = "1.0.0"
				d.ManifestDigest = m.Digest
				d.Key = definitionKey(m.Digest, card.Operation, card.Slots, slotRevisions, card.Params)
				params, _ := json.Marshal(card.Params)
				d.Run = func(c context.Context) (widgets.Document, error) {
					r, e := inst.Invoke(c, integrations.InvokeRequest{Operation: "request", Params: params, Grant: g.Fresh()})
					return r.Document, e
				}
				built.Definitions = append(built.Definitions, d)
				continue
			}
			in, ok := snap.IntegrationByID(card.Integration)
			if !ok {
				d.Disabled = state.ReasonIntegrationMissing
				built.Definitions = append(built.Definitions, d)
				continue
			}
			src, e := integrations.ResolveSource(configDir, in.Source)
			if e != nil || src.Builtin {
				d.Disabled = state.ReasonIntegrationMissing
				built.Definitions = append(built.Definitions, d)
				continue
			}
			path, e := integrations.ManifestFile(src.Dir)
			if e != nil {
				d.Disabled = state.ReasonIntegrationMissing
				built.Definitions = append(built.Definitions, d)
				continue
			}
			m, e := manifestload.Load(path)
			if e != nil {
				d.Disabled = state.ReasonConfigError
				built.Definitions = append(built.Definitions, d)
				continue
			}
			op := operation(m, card.Operation)
			if op == nil {
				d.Disabled = state.ReasonConfigError
				built.Definitions = append(built.Definitions, d)
				continue
			}
			d.SignalTypes = signalTypes(op.Signals)
			d.HistorySignals = historicalSignals(op.Signals)
			entry := lock.Integrations[in.ID]
			if entry == nil {
				d.Disabled = state.ReasonUnapproved
				built.Definitions = append(built.Definitions, d)
				continue
			}
			if entry.ManifestSHA256 != m.Digest {
				d.Disabled = state.ReasonPermissionsChanged
				built.Definitions = append(built.Definitions, d)
				continue
			}
			d.ApprovalRevision, err = storage.DefinitionHash(entry)
			if err != nil {
				return nil, err
			}
			var runtime integrations.Runtime
			switch m.Runtime {
			case "declarative":
				runtime = declarativeRuntime
			case "wasm":
				if wasmRuntime == nil {
					wasmRuntime, e = wasmrt.NewWithBroker(ctx, wasmCacheDir, broker)
					if e != nil {
						return nil, e
					}
					built.runtimes = append(built.runtimes, wasmRuntime)
				}
				runtime = wasmRuntime
			default:
				d.Disabled = state.ReasonConfigError
				built.Definitions = append(built.Definitions, d)
				continue
			}
			inst, e := runtime.Load(ctx, integrations.Installed{Manifest: m, Lock: entry})
			if e != nil {
				return nil, e
			}
			if _, seen := loaded[in.ID]; !seen {
				built.PluginLoads = append(built.PluginLoads, PluginLoad{ID: in.ID, Runtime: m.Runtime, Version: m.Version, Digest: m.Digest})
				loaded[in.ID] = struct{}{}
			}
			g := grant(m, entry, card, reg, revisions, generation, d.ApprovalRevision, d.Hash)
			d.Source.Runtime = state.Runtime(m.Runtime)
			d.Source.IntegrationVersion = m.Version
			d.ManifestDigest = m.Digest
			d.Key = definitionKey(m.Digest, card.Operation, card.Slots, slotRevisions, card.Params)
			d.Timeout = time.Duration(entry.EffectiveLimits.TimeoutMs) * time.Millisecond
			params, _ := json.Marshal(card.Params)
			operationID := card.Operation
			d.Run = func(c context.Context) (widgets.Document, error) {
				r, e := inst.Invoke(c, integrations.InvokeRequest{Operation: operationID, Params: params, Grant: g.Fresh()})
				return r.Document, e
			}
			built.Definitions = append(built.Definitions, d)
		}
	}
	built.RuleDeclarations = make(map[string]rules.CardDefinition, len(built.Definitions))
	for _, definition := range built.Definitions {
		built.RuleDeclarations[definition.ID] = rules.CardDefinition{Hash: definition.Hash, Signals: definition.SignalTypes}
	}
	for _, rule := range snap.Config.Rules {
		definition, compileErr := rules.Compile(rule, built.RuleDeclarations)
		if compileErr != nil {
			return nil, compileErr
		}
		built.RuleDefinitions = append(built.RuleDefinitions, definition)
	}
	return built, nil
}

func signalTypes(signals []manifestload.SignalDecl) map[string]string {
	out := make(map[string]string, len(signals))
	for _, signal := range signals {
		out[signal.Name] = signal.Type
	}
	return out
}

func historicalSignals(signals []manifestload.SignalDecl) map[string]struct{} {
	out := make(map[string]struct{})
	for _, signal := range signals {
		if signal.History && signal.Type == "number" {
			out[signal.Name] = struct{}{}
		}
	}
	return out
}

func grant(m *manifestload.Manifest, l *integrations.LockEntry, c config.Card, reg connections.Registry, revisions map[string]string, generation uint64, approvalRevision, cardHash string) capabilities.Grant {
	mr := []capabilities.Route{}
	for _, o := range m.Operations {
		for _, r := range o.Routes {
			mr = append(mr, capabilities.Route{Slot: r.Slot, Method: r.Method, Path: r.Path, Use: runtimeUse(r.Use), QueryKeys: r.QueryKeys, ContentType: r.ContentType, MaxBodyKB: r.MaxBodyKB})
		}
	}
	ar := []capabilities.Route{}
	for _, r := range l.Routes {
		ar = append(ar, capabilities.Route{Slot: r.Slot, Method: r.Method, Path: r.Path, Use: runtimeUse(r.Use), QueryKeys: r.QueryKeys, ContentType: r.ContentType, MaxBodyKB: r.MaxBodyKB})
	}
	pol := map[string]capabilities.ConnectionPolicy{}
	for slot, id := range c.Slots {
		if x, ok := reg.Get(id); ok && x.HTTP != nil {
			pol[slot] = capabilities.ConnectionPolicy{AllowedPaths: x.HTTP.AllowedPaths}
		}
	}
	e := l.EffectiveLimits
	return capabilities.NewGrant(m.ID, m.Version, c.ID, c.Slots, capabilities.NewCapSet(l.Capabilities...), mr, ar, pol, capabilities.Limits{HTTPRequests: e.HTTPRequests, ResponseMB: e.ResponseMB, CacheEntries: e.CacheEntries, CacheBytesKB: e.CacheBytesKB, HostCalls: e.HostCalls, RequestBodyKB: e.RequestBodyKB}, capabilities.ExecutionIdentity{SnapshotGen: generation, CardHash: cardHash, ManifestDigest: m.Digest, ApprovalRevision: approvalRevision, SlotRevisions: revisionsFor(c.Slots, revisions)})
}
func operation(m *manifestload.Manifest, id string) *manifestload.OperationDef {
	for i := range m.Operations {
		if m.Operations[i].ID == id {
			return &m.Operations[i]
		}
	}
	return nil
}
func firstSlot(m map[string]string) (string, string) {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return keys[0], m[keys[0]]
	}
	return "server", ""
}
func stringParam(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func stringMapParam(m map[string]any, k string) map[string]string {
	out := map[string]string{}
	v, _ := m[k].(map[string]any)
	for a, b := range v {
		if s, ok := b.(string); ok {
			out[a] = s
		}
	}
	return out
}
func parseDuration(v string, d time.Duration) time.Duration {
	x, e := time.ParseDuration(v)
	if e != nil || x <= 0 {
		return d
	}
	return x
}
func definitionKey(digest, op string, slots, revisions map[string]string, params map[string]any) string {
	h, e := storage.DefinitionHash(struct {
		Digest, Operation string
		Slots             map[string]string
		Revisions         map[string]string
		Params            map[string]any
	}{digest, op, slots, revisions, params})
	if e != nil {
		return digest + ":" + op
	}
	return h
}

func revisionsFor(slots, revisions map[string]string) map[string]string {
	out := make(map[string]string, len(slots))
	for slot, connectionID := range slots {
		out[slot] = revisions[connectionID]
	}
	return out
}

func runtimeUse(value string) capabilities.UseKind {
	if value == "" {
		return capabilities.UseData
	}
	return capabilities.UseKind(value)
}
