// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/integrations"
)

// Milestone D2b: GET /api/v1/integrations, GET /api/v1/integrations/{id}/approval and
// POST /api/v1/integrations/{id}/approve - the REST side of the same two-step, digest-bound
// approval transaction `veduta integration approve` performs (cmd/veduta/integration.go).
//
// Scope note: docs/02-implementation-plan.md's D2b entry also asks for a sudo-window
// re-authentication gate on the approve endpoint and an audited approval trail. Neither exists:
// sessions are milestone H1 (deps: F1, not built) and internal/audit is H2 (deps: H1). Gating
// approval on a sudo window that cannot exist yet would either be unbuildable or fake, so this
// endpoint currently has no additional gate beyond whatever reaches it at all - unauthenticated
// access is already bounded by the loopback-only bind this server refuses to lift before
// authentication exists (see Config.AllowPublicWithoutAuth). The real gap - wiring a sudo check
// and an audit write here - is recorded in docs/03-backlog.md with this file named as the hook
// point for H1/H2 to extend.
//
// integrationSummary is one row of GET /api/v1/integrations - docs/01-architecture.md section 9:
// "installed integrations: version, runtime, lock status (approved/unapproved/changed), granted
// capabilities and routes." Capabilities is what is actually GRANTED (from the lock), not what
// the manifest requests - that distinction is what makes this endpoint safe to show without a
// diff attached to every row.
type integrationSummary struct {
	ID           string   `json:"id"`
	Builtin      bool     `json:"builtin"`
	Status       string   `json:"status,omitempty"`
	Runtime      string   `json:"runtime,omitempty"`
	Version      string   `json:"version,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// approvalPreview is GET /api/v1/integrations/{id}/approval's body - docs/01-architecture.md
// section 6's exact shape: "{ manifestSha256, currentLock, diff: {...} }", plus ManifestLimits
// (what the manifest itself requests) so a client that wants to approve everything currently
// requested - the common case - can echo it straight back as the POST body's grants.limits
// without separately re-deriving it. Limits above the documented default are only actually
// granted if grants.limits says so explicitly (see internal/integrations.ReconcileAtApproval).
type approvalPreview struct {
	ManifestSHA256 string                  `json:"manifestSha256"`
	CurrentLock    *integrations.LockEntry `json:"currentLock"`
	Diff           integrations.Diff       `json:"diff"`
	ManifestLimits integrations.Limits     `json:"manifestLimits"`
}

// approveRequest is POST /api/v1/integrations/{id}/approve's body - docs/01-architecture.md
// section 6: "the client sends the exact grants it is approving, not a bare yes."
type approveRequest struct {
	ExpectedManifestSHA256 string              `json:"expectedManifestSha256"`
	Grants                 integrations.Grants `json:"grants"`
	ApprovedBy             string              `json:"approvedBy,omitempty"`
}

// errIntegrationNotDeclared and errIntegrationBuiltin distinguish "there is nothing to approve
// here" from an ordinary manifest load failure, so the handler can pick the right status code.
var (
	errIntegrationNotDeclared = errors.New("integration not declared in configuration")
	errIntegrationBuiltin     = errors.New("builtin integrations are exempt from the lock")
)

// resolvedIntegration is what every handler below needs, loaded once: the manifest it should
// compare against, the lock entry it currently has (nil if unapproved), and where to write a new
// one back to.
type resolvedIntegration struct {
	manifest *integrations.Manifest
	entry    *integrations.LockEntry
	lockPath string
}

func (s *Server) resolveForApproval(id string) (*resolvedIntegration, error) {
	store := s.cfg.ConfigStore
	snapshot := store.Snapshot()
	if snapshot == nil {
		return nil, errors.New("no valid configuration loaded")
	}
	in, ok := snapshot.IntegrationByID(id)
	if !ok {
		return nil, errIntegrationNotDeclared
	}
	configDir := filepath.Dir(store.Path())
	src, err := integrations.ResolveSource(configDir, in.Source)
	if err != nil {
		return nil, err
	}
	if src.Builtin {
		return nil, errIntegrationBuiltin
	}
	manifest, err := integrations.LoadManifest(src.Dir)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(configDir, integrations.LockFileName)
	lock, err := integrations.ReadLock(lockPath)
	if err != nil {
		return nil, err
	}
	return &resolvedIntegration{manifest: manifest, entry: lock.Integrations[id], lockPath: lockPath}, nil
}

func (s *Server) routeIntegrations(mux *http.ServeMux) {
	store := s.cfg.ConfigStore
	mux.HandleFunc("GET /api/v1/integrations", func(w http.ResponseWriter, r *http.Request) {
		snapshot := store.Snapshot()
		if snapshot == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no valid configuration loaded"})
			return
		}
		configDir := filepath.Dir(store.Path())
		lock, err := integrations.ReadLock(filepath.Join(configDir, integrations.LockFileName))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ids := make([]string, 0, len(snapshot.Config.Integrations))
		for _, in := range snapshot.Config.Integrations {
			ids = append(ids, in.ID)
		}
		sort.Strings(ids)
		out := make([]integrationSummary, 0, len(ids))
		for _, id := range ids {
			out = append(out, summariseIntegration(snapshot, lock, configDir, id))
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/integrations/{id}/approval", func(w http.ResponseWriter, r *http.Request) {
		resolved, err := s.resolveForApproval(r.PathValue("id"))
		if err != nil {
			writeIntegrationError(w, err)
			return
		}
		diff, err := integrations.ComputeDiff(resolved.manifest, resolved.entry)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, approvalPreview{
			ManifestSHA256: resolved.manifest.Digest,
			CurrentLock:    resolved.entry,
			Diff:           diff,
			ManifestLimits: resolved.manifest.Limits,
		})
	})

	mux.HandleFunc("POST /api/v1/integrations/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		resolved, err := s.resolveForApproval(id)
		if err != nil {
			writeIntegrationError(w, err)
			return
		}

		var req approveRequest
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + decodeErr.Error()})
			return
		}

		entry, err := integrations.Approve(resolved.manifest, req.ExpectedManifestSHA256, req.Grants, req.ApprovedBy, time.Now())
		if err != nil {
			if errors.Is(err, integrations.ErrDigestChanged) {
				// docs/01-architecture.md section 6: "409 Conflict with the new diff. Nothing is
				// approved." resolved.entry is still the PRE-attempt lock record (nothing was
				// written), which is exactly what the fresh diff must be computed against.
				diff, diffErr := integrations.ComputeDiff(resolved.manifest, resolved.entry)
				if diffErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": diffErr.Error()})
					return
				}
				writeJSON(w, http.StatusConflict, approvalPreview{ManifestSHA256: resolved.manifest.Digest, CurrentLock: resolved.entry, Diff: diff})
				return
			}
			if errors.Is(err, integrations.ErrGrantExceedsRequest) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		lock, err := integrations.ReadLock(resolved.lockPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		lock.Integrations[id] = entry
		if err := integrations.WriteLock(resolved.lockPath, lock); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, entry)
	})
}

func writeIntegrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errIntegrationNotDeclared):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errIntegrationBuiltin):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		var notFound *integrations.ErrManifestNotFound
		if errors.As(err, &notFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func summariseIntegration(snapshot *config.Snapshot, lock *integrations.Lock, configDir, id string) integrationSummary {
	in, _ := snapshot.IntegrationByID(id)
	src, err := integrations.ResolveSource(configDir, in.Source)
	if err != nil {
		return integrationSummary{ID: id, Error: err.Error()}
	}
	if src.Builtin {
		return integrationSummary{ID: id, Builtin: true, Status: string(integrations.StatusBuiltin)}
	}
	m, err := integrations.LoadManifest(src.Dir)
	if err != nil {
		return integrationSummary{ID: id, Error: err.Error()}
	}
	entry := lock.Integrations[id]
	summary := integrationSummary{
		ID:      id,
		Status:  string(integrations.Evaluate(m, entry)),
		Runtime: m.Runtime,
		Version: m.Version,
	}
	if entry != nil {
		summary.Capabilities = entry.Capabilities
	}
	return summary
}
