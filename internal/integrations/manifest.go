// SPDX-License-Identifier: AGPL-3.0-or-later

// Package integrations implements milestone D2b: the integration lock and approval flow that
// makes "safe to install from strangers" enforceable without a marketplace. A manifest is a
// request for authority (docs/01-architecture.md section 6); nothing here takes effect until it
// is recorded in veduta.lock.yaml.
//
// Scope note: docs/02-implementation-plan.md's D2b entry also names sudo-window enforcement and
// an audited approval trail, both of which need sessions (H1) and internal/audit (H2) - neither
// exists yet (H1 depends on F1, which does not exist either). This package implements the part
// that has no such dependency: the canonical digest, the lock file, the permission diff, and CLI
// plus REST access to approve. The sudo/audit gap is recorded in docs/03-backlog.md rather than
// silently built against non-existent sessions or silently dropped.
package integrations

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"veduta.dev/veduta/internal/canonical"
)

// Manifest is the subset of a plugin manifest (schemas/plugin-manifest.v1.schema.json) that
// approval needs: what it asks for, not how its operations actually run - the pipeline/output
// template grammar belongs to milestone D3's own loader, not this one. LoadManifest always
// computes Digest through internal/canonical, the single strict decoder both this package and
// `veduta manifest digest` depend on, rather than re-deriving parsing strictness (duplicate key
// rejection, bounded YAML aliases, integer-only numbers) here.
type Manifest struct {
	ID           string
	Name         string
	Version      string
	Runtime      string // declarative | wasm | builtin
	Capabilities []string
	Limits       Limits
	Routes       []Route // aggregated across every operation, exact duplicates dropped
	ModuleSHA256 string  // wasm only; "" otherwise
	Digest       string  // canonical manifest digest - what the lock records and pins against
}

// manifestDoc mirrors just enough of the manifest schema to extract Manifest's fields via an
// ordinary YAML decode. It does not enforce the schema (D3's manifestload does that once the
// declarative runtime exists) - LoadManifest's own safety comes from requiring
// canonical.DigestFile to succeed first, which already rejects a malformed or ambiguous document.
type manifestDoc struct {
	Metadata struct {
		ID      string `yaml:"id"`
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	Spec struct {
		Runtime      string   `yaml:"runtime"`
		Capabilities []string `yaml:"capabilities"`
		Limits       Limits   `yaml:"limits"`
		Module       string   `yaml:"module"`
		SHA256       string   `yaml:"sha256"`
		Operations   []struct {
			Routes []Route `yaml:"routes"`
		} `yaml:"operations"`
	} `yaml:"spec"`
}

// ErrManifestNotFound is returned when a resolved integration source has no manifest.yaml or
// manifest.yml - distinguished from other load errors so callers (the CLI, the REST handlers)
// can report "not installed" rather than a parse failure.
type ErrManifestNotFound struct{ Dir string }

func (e *ErrManifestNotFound) Error() string {
	return fmt.Sprintf("no manifest.yaml or manifest.yml in %s", e.Dir)
}

// findManifestFile looks for manifest.yaml then manifest.yml in dir - plugins/{immich,jellyfin,
// glances} all ship the former; both are accepted for the same reason config.Load accepts either
// extension.
func findManifestFile(dir string) (string, error) {
	for _, name := range []string{"manifest.yaml", "manifest.yml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", &ErrManifestNotFound{Dir: dir}
}

// LoadManifest loads and digests the manifest in dir.
func LoadManifest(dir string) (*Manifest, error) {
	path, err := findManifestFile(dir)
	if err != nil {
		return nil, err
	}
	digest, err := canonical.DigestFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// #nosec G304 -- path was just located by findManifestFile within an operator-configured
	// integration directory, not attacker-controlled request input.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var doc manifestDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var routes []Route
	for _, op := range doc.Spec.Operations {
		routes = append(routes, op.Routes...)
	}

	return &Manifest{
		ID:           doc.Metadata.ID,
		Name:         doc.Metadata.Name,
		Version:      doc.Metadata.Version,
		Runtime:      doc.Spec.Runtime,
		Capabilities: doc.Spec.Capabilities,
		Limits:       doc.Spec.Limits,
		Routes:       dedupeRoutes(routes),
		ModuleSHA256: doc.Spec.SHA256,
		Digest:       digest,
	}, nil
}
