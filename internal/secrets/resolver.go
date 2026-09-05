// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"fmt"

	"veduta.dev/veduta/internal/config"
)

// Resolver tries Providers in order for every name, returning the first hit. Order matters:
// docs/01-architecture.md section 2 specifies file: then env:, so a Docker secret shadows an
// environment variable of the same name rather than the other way around.
type Resolver struct {
	Providers []Provider
}

// DefaultResolver is file: (/run/secrets, the Docker secrets mount point) then env:.
func DefaultResolver() Resolver {
	return Resolver{Providers: []Provider{
		FileProvider{Dir: "/run/secrets"},
		EnvProvider{},
	}}
}

// Resolve tries each Provider in order, returning the first hit. A Provider error stops the
// search and is returned directly - a real problem (permission denied) should not be silently
// papered over by falling through to the next provider.
func (r Resolver) Resolve(name string) (Value, bool, error) {
	for _, p := range r.Providers {
		v, ok, err := p.Resolve(name)
		if err != nil {
			return Value{}, false, err
		}
		if ok {
			return New(v), true, nil
		}
	}
	return Value{}, false, nil
}

// ResolveAll resolves every ${secret:NAME} reference config.Load found (Snapshot.SecretRefs),
// returning a lookup by name and a Diagnostic for every occurrence of a name that could not be
// resolved - not one anonymous failure, but every real file:line:col the config referenced it
// from, reusing internal/config's own Diagnostic type rather than inventing a parallel one.
func ResolveAll(refs []config.SecretLocation, r Resolver) (map[string]Value, config.Diagnostics) {
	resolved := make(map[string]Value)
	failed := make(map[string]error)
	tried := make(map[string]bool)

	for _, ref := range refs {
		if tried[ref.Name] {
			continue
		}
		tried[ref.Name] = true
		v, ok, err := r.Resolve(ref.Name)
		switch {
		case err != nil:
			failed[ref.Name] = err
		case ok:
			resolved[ref.Name] = v
		default:
			failed[ref.Name] = nil
		}
	}

	var diags config.Diagnostics
	for _, ref := range refs {
		err, isFailure := failed[ref.Name]
		if !isFailure {
			continue
		}
		msg := fmt.Sprintf("secret %q could not be resolved (no file or environment variable provides it)", ref.Name)
		if err != nil {
			msg = fmt.Sprintf("secret %q: %s", ref.Name, err.Error())
		}
		diags = append(diags, config.Diagnostic{
			Severity: config.SeverityError,
			File:     ref.File,
			Line:     ref.Line,
			Column:   ref.Column,
			Message:  msg,
		})
	}
	return resolved, diags
}
