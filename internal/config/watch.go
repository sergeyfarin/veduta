// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const reloadDebounce = 300 * time.Millisecond
const defaultSecretDir = "/run/secrets"

// Loader performs the complete load-time pipeline. Callers may wrap LoadPath to add steps such
// as secret resolution; a failed step must return a nil snapshot and diagnostics.
type Loader func(path string) (*Snapshot, Diagnostics)

// Activator prepares and swaps every runtime component derived from a candidate snapshot. It is
// called before Store publishes that snapshot; returning an error keeps the previous generation
// live and exposes the failure through Status.
type Activator func(context.Context, *Snapshot, uint64) error

// Status is the last config load result. A failed reload updates this value but never replaces
// the live snapshot or its checksum/generation.
type Status struct {
	OK          bool        `json:"ok"`
	Generation  uint64      `json:"generation"`
	Checksum    string      `json:"checksum,omitempty"`
	LoadedAt    time.Time   `json:"loadedAt,omitempty"`
	AttemptedAt time.Time   `json:"attemptedAt"`
	Diagnostics Diagnostics `json:"diagnostics"`
}

// Store owns the process-wide immutable config snapshot and its live-reload status.
type Store struct {
	path     string
	load     Loader
	log      *slog.Logger
	current  atomic.Pointer[Snapshot]
	mu       sync.RWMutex
	status   Status
	activate Activator
}

// Open loads the initial snapshot. The server must not start without one valid configuration.
func Open(path string, logger *slog.Logger, loader Loader) (*Store, Diagnostics) {
	if logger == nil {
		logger = slog.Default()
	}
	if loader == nil {
		loader = LoadPath
	}
	s := &Store{path: filepath.Clean(path), load: loader, log: logger}
	snapshot, diags := loader(s.path)
	now := time.Now().UTC()
	if snapshot == nil || diags.HasErrors() {
		s.status = Status{OK: false, AttemptedAt: now, Diagnostics: copyDiagnostics(diags)}
		return s, diags
	}
	s.current.Store(snapshot)
	s.status = Status{OK: true, Generation: 1, Checksum: snapshotChecksum(snapshot), LoadedAt: now, AttemptedAt: now, Diagnostics: copyDiagnostics(diags)}
	return s, diags
}

// Snapshot returns the current immutable snapshot. It is safe and lock-free for concurrent use.
func (s *Store) Snapshot() *Snapshot { return s.current.Load() }

// Path returns the primary config file path this Store was opened with - callers that need to
// locate a file conventionally sited next to it (veduta.lock.yaml, a `path:` integration source)
// resolve relative to its directory rather than guessing the working directory.
func (s *Store) Path() string { return s.path }

// Status returns a copy of the latest load result.
func (s *Store) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.status
	out.Diagnostics = copyDiagnostics(out.Diagnostics)
	return out
}

// SetActivator installs the runtime activation hook used by subsequent reloads. Startup builds
// its first runtime before the HTTP server becomes reachable, then installs this hook before Watch.
func (s *Store) SetActivator(activate Activator) {
	s.mu.Lock()
	s.activate = activate
	s.mu.Unlock()
}

// Watch blocks until ctx is cancelled, reloading after relevant filesystem events settle.
func (s *Store) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch config: %w", err)
	}
	defer func() { _ = w.Close() }()

	parent := filepath.Dir(s.path)
	if err := w.Add(parent); err != nil {
		return fmt.Errorf("watch config directory %s: %w", parent, err)
	}
	confDir := filepath.Join(parent, "conf.d")
	watchConfDir := func() {
		if err := w.Add(confDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("could not watch config fragment directory", "path", confDir, "error", err)
		}
	}
	watchConfDir()
	watchedIntegrationDirs := map[string]bool{}
	watchIntegrationDirs := func() {
		snapshot := s.Snapshot()
		if snapshot == nil {
			return
		}
		for _, integration := range snapshot.Config.Integrations {
			relative, ok := strings.CutPrefix(integration.Source, "path:")
			if !ok || relative == "" {
				continue
			}
			dir := relative
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(parent, dir)
			}
			dir = filepath.Clean(dir)
			if watchedIntegrationDirs[dir] {
				continue
			}
			if err := w.Add(dir); err != nil {
				s.log.Warn("could not watch integration directory", "path", dir, "error", err)
				continue
			}
			watchedIntegrationDirs[dir] = true
		}
	}
	watchIntegrationDirs()
	if err := w.Add(defaultSecretDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warn("could not watch file-secret directory", "path", defaultSecretDir, "error", err)
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	schedule := func() {
		if timer == nil {
			timer = time.NewTimer(reloadDebounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(reloadDebounce)
		}
		timerC = timer.C
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			s.log.Warn("config watcher error", "error", err)
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if filepath.Clean(event.Name) == confDir && event.Op&fsnotify.Create != 0 {
				watchConfDir()
			}
			if s.relevant(event.Name) {
				schedule()
			}
		case <-timerC:
			timerC = nil
			s.reload(ctx)
			watchIntegrationDirs()
		}
	}
}

func (s *Store) relevant(name string) bool {
	name = filepath.Clean(name)
	if name == s.path {
		return true
	}
	if filepath.Dir(name) == filepath.Dir(s.path) && filepath.Base(name) == "veduta.lock.yaml" {
		return true
	}
	confDir := filepath.Join(filepath.Dir(s.path), "conf.d")
	if filepath.Dir(name) == confDir && filepath.Ext(name) == ".yaml" {
		return true
	}
	if filepath.Dir(name) == defaultSecretDir {
		// Secret mounts commonly rotate an internal `..data` symlink rather than emitting an
		// event named after the referenced file. Any event in the small secrets directory is
		// therefore relevant when this config uses at least one file-capable reference.
		return len(s.Snapshot().SecretRefs) > 0
	}
	base := filepath.Base(name)
	if base == "manifest.yaml" || base == "manifest.yml" {
		for _, integration := range s.Snapshot().Config.Integrations {
			relative, ok := strings.CutPrefix(integration.Source, "path:")
			if !ok {
				continue
			}
			dir := relative
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(filepath.Dir(s.path), dir)
			}
			if filepath.Clean(dir) == filepath.Dir(name) {
				return true
			}
		}
	}
	return false
}

func (s *Store) reload(ctx context.Context) {
	snapshot, diags := s.load(s.path)
	now := time.Now().UTC()
	s.mu.RLock()
	nextGeneration := s.status.Generation + 1
	activate := s.activate
	s.mu.RUnlock()
	if snapshot != nil && !diags.HasErrors() && activate != nil {
		if err := activate(ctx, snapshot, nextGeneration); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, File: s.path, Message: "activate runtime: " + err.Error()})
			snapshot = nil
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.AttemptedAt = now
	s.status.Diagnostics = copyDiagnostics(diags)
	if snapshot == nil || diags.HasErrors() {
		s.status.OK = false
		s.log.Error("config reload rejected; keeping previous configuration", "diagnostics", diags.String())
		return
	}
	s.current.Store(snapshot)
	s.status.OK = true
	s.status.Generation = nextGeneration
	s.status.Checksum = snapshotChecksum(snapshot)
	s.status.LoadedAt = now
	s.log.Info("configuration reloaded", "generation", s.status.Generation, "checksum", s.status.Checksum)
}

func snapshotChecksum(snapshot *Snapshot) string {
	body, err := json.Marshal(snapshot.Config)
	if err != nil {
		panic("config: schema-valid snapshot cannot fail JSON encoding: " + err.Error())
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func copyDiagnostics(in Diagnostics) Diagnostics {
	if in == nil {
		return Diagnostics{}
	}
	return append(Diagnostics(nil), in...)
}
