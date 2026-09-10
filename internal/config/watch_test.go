// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreReloadKeepsLastGoodAndRecoversAfterReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	writeWatchConfig(t, path, "first")
	store, diags := Open(path, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	first := store.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- store.Watch(ctx) }()
	time.Sleep(50 * time.Millisecond) // let the watcher attach before the first edit

	// An invalid edit updates status but cannot replace the live snapshot.
	if err := os.WriteFile(path, []byte("version: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, func(s Status) bool { return !s.OK && len(s.Diagnostics) > 0 })
	if store.Snapshot() != first {
		t.Fatal("invalid reload replaced the last-good snapshot")
	}

	// Rename-and-replace is how common editors save; watching the directory must survive it.
	tmp := filepath.Join(dir, ".veduta.yaml.tmp")
	writeWatchConfig(t, tmp, "second")
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, func(s Status) bool { return s.OK && s.Generation == 2 })
	if got := store.Snapshot().Config.Dashboard.Title; got != "second" {
		t.Fatalf("title after replacement = %q, want second", got)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Watch did not stop after cancellation")
	}
}

func TestStoreRapidEditsCoalesce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	writeWatchConfig(t, path, "initial")
	store, diags := Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = store.Watch(ctx) }()
	time.Sleep(50 * time.Millisecond) // let the watcher attach before generating the burst
	for i := 0; i < 4; i++ {
		writeWatchConfig(t, path, fmt.Sprintf("edit-%d", i))
		time.Sleep(40 * time.Millisecond)
	}
	waitForStatus(t, store, func(s Status) bool { return s.Generation == 2 })
	time.Sleep(reloadDebounce + 100*time.Millisecond)
	if got := store.Status().Generation; got != 2 {
		t.Fatalf("generation = %d, want one coalesced reload (2)", got)
	}
}

func TestStoreReloadsWhenApprovalLockChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	writeWatchConfig(t, path, "initial")
	store, diags := Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = store.Watch(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "veduta.lock.yaml"), []byte("version: 1\nintegrations: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, store, func(s Status) bool { return s.Generation == 2 })
}

func TestStoreActivatesCandidateBeforePublishingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	writeWatchConfig(t, path, "first")
	store, diags := Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}
	first := store.Snapshot()
	store.SetActivator(func(_ context.Context, candidate *Snapshot, generation uint64) error {
		if store.Snapshot() != first {
			t.Fatal("candidate was published before activation completed")
		}
		if candidate.Config.Dashboard.Title != "rejected" || generation != 2 {
			t.Fatalf("activation candidate = %q generation %d", candidate.Config.Dashboard.Title, generation)
		}
		return errors.New("runtime composition failed")
	})
	writeWatchConfig(t, path, "rejected")
	store.reload(context.Background())
	if store.Snapshot() != first {
		t.Fatal("failed activation replaced the live snapshot")
	}
	status := store.Status()
	if status.OK || status.Generation != 1 || len(status.Diagnostics) != 1 {
		t.Fatalf("failed activation status = %+v", status)
	}

	store.SetActivator(func(_ context.Context, candidate *Snapshot, generation uint64) error {
		if store.Snapshot() != first || candidate.Config.Dashboard.Title != "accepted" || generation != 2 {
			t.Fatal("activation did not receive the unpublished next generation")
		}
		return nil
	})
	writeWatchConfig(t, path, "accepted")
	store.reload(context.Background())
	if got := store.Snapshot().Config.Dashboard.Title; got != "accepted" {
		t.Fatalf("published title = %q", got)
	}
	if status = store.Status(); !status.OK || status.Generation != 2 {
		t.Fatalf("accepted activation status = %+v", status)
	}
}

func waitForStatus(t *testing.T, store *Store, ready func(Status) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ready(store.Status()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status; last = %+v", store.Status())
}

func writeWatchConfig(t *testing.T, path, title string) {
	t.Helper()
	body := fmt.Sprintf(`version: 1
server:
  listen: 127.0.0.1:8099
auth:
  mode: none
dashboard:
  title: %s
  appearance: clean
  layout: {columns: 4, gap: normal}
  groupBy: section
connections: {}
integrations: []
sections: []
rules: []
notifications: {channels: {}}
`, title)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
