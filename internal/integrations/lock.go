// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LockFileName is veduta.lock.yaml's conventional name, written next to the primary config file
// - docs/01-architecture.md section 6.
const LockFileName = "veduta.lock.yaml"

// LockEntry is one integration's approval record - schemas/integration-lock.v1.schema.json's
// per-id object. Field names match the schema exactly.
type LockEntry struct {
	ManifestSHA256  string          `yaml:"manifestSha256" json:"manifestSha256"`
	ModuleSHA256    string          `yaml:"moduleSha256,omitempty" json:"moduleSha256,omitempty"`
	Version         string          `yaml:"version,omitempty" json:"version,omitempty"`
	Runtime         string          `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	ApprovedAt      string          `yaml:"approvedAt" json:"approvedAt"`
	ApprovedBy      string          `yaml:"approvedBy,omitempty" json:"approvedBy,omitempty"`
	Capabilities    []string        `yaml:"capabilities" json:"capabilities"`
	Routes          []Route         `yaml:"routes" json:"routes"`
	Limits          *Limits         `yaml:"limits,omitempty" json:"limits,omitempty"`
	EffectiveLimits EffectiveLimits `yaml:"effectiveLimits" json:"effectiveLimits"`
}

// Lock is the decoded veduta.lock.yaml document.
type Lock struct {
	Version      int                   `yaml:"version" json:"version"`
	Integrations map[string]*LockEntry `yaml:"integrations" json:"integrations"`
}

// emptyLock is what a missing lock file means: no integration has ever been approved.
func emptyLock() *Lock {
	return &Lock{Version: 1, Integrations: map[string]*LockEntry{}}
}

// ReadLock loads path, or returns an empty Lock if it does not exist - a fresh installation has
// no lock file at all, and that is not an error.
func ReadLock(path string) (*Lock, error) {
	// #nosec G304 -- path is the operator's own configuration location (next to veduta.yaml),
	// not attacker-controlled request input.
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyLock(), nil
		}
		return nil, err
	}
	var generic any
	if err = yaml.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sch, err := lockSchema()
	if err != nil {
		return nil, err
	}
	if err = sch.Validate(generic); err != nil {
		return nil, fmt.Errorf("%s does not match the lock schema: %w", path, err)
	}
	var lock Lock
	if err = yaml.Unmarshal(raw, &lock); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if lock.Integrations == nil {
		lock.Integrations = map[string]*LockEntry{}
	}
	return &lock, nil
}

// WriteLock schema-validates lock, then writes it to path atomically (write a temp file in the
// same directory, then rename) so a crash or a concurrent reader never observes a half-written
// lock file - the file an administrator is told to commit alongside veduta.yaml.
func WriteLock(path string, lock *Lock) error {
	body, err := yaml.Marshal(lock)
	if err != nil {
		return fmt.Errorf("marshalling lock: %w", err)
	}
	var generic any
	if err = yaml.Unmarshal(body, &generic); err != nil {
		return fmt.Errorf("internal: re-parsing marshalled lock: %w", err)
	}
	sch, err := lockSchema()
	if err != nil {
		return err
	}
	if err = sch.Validate(generic); err != nil {
		return fmt.Errorf("internal: lock about to be written does not match its own schema: %w", err)
	}

	header := "# Written by `veduta integration approve <id>`; commit it alongside veduta.yaml.\n" +
		"# A manifest whose digest or effective permissions differ from this record is\n" +
		"# refused at load with a printed permission diff, until it is approved again.\n"
	final := append([]byte(header), body...)

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".veduta.lock.yaml.*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(final); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming into place: %w", err)
	}
	return nil
}
