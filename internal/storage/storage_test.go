// SPDX-License-Identifier: AGPL-3.0-or-later

package storage_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

func TestMigrationsAreCompleteAndIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		got[n] = true
	}
	_ = rows.Close()
	for _, n := range []string{"schema_migrations", "settings", "users", "sessions", "card_state", "signal_history", "events", "plugin_kv", "connection_state", "asset_cache", "rule_state", "notifications", "audit_log"} {
		if !got[n] {
			t.Errorf("missing table %s", n)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s.Close()
}

func TestCardDefinitionChangeDiscardsRetainedState(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	doc := widgets.Document{Blocks: []widgets.Block{}}
	cs := state.OK("card", doc, state.Source{Integration: "x", Runtime: state.RuntimeDeclarative, Operation: "op"}, time.Minute, time.Millisecond)
	if err = s.PutCard(ctx, storage.CardRecord{CardHash: "old", State: cs}); err != nil {
		t.Fatal(err)
	}
	if _, ok, getErr := s.GetCard(ctx, "card", "new"); getErr != nil || ok {
		t.Fatalf("changed definition retained state: ok=%v err=%v", ok, getErr)
	}
	if _, ok, getErr := s.GetCard(ctx, "card", "old"); getErr != nil || ok {
		t.Fatalf("discard was not persistent: ok=%v err=%v", ok, getErr)
	}
}

func TestSettingPersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Setting(ctx, "asset_hmac_key", 32)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := s.Setting(ctx, "asset_hmac_key", 32)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("setting changed across restart")
	}
}

func TestConnectionRevisionStableThenRotatesAndDeletes(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.SyncConnectionRevisions(ctx, map[string][]byte{"photos": []byte("material-a")})
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.SyncConnectionRevisions(ctx, map[string][]byte{"photos": []byte("material-a")})
	if err != nil {
		t.Fatal(err)
	}
	if first["photos"] != same["photos"] {
		t.Fatal("unchanged connection revision rotated")
	}
	changed, err := s.SyncConnectionRevisions(ctx, map[string][]byte{"photos": []byte("material-b")})
	if err != nil {
		t.Fatal(err)
	}
	if changed["photos"] == first["photos"] {
		t.Fatal("changed connection revision did not rotate")
	}
	if _, err = s.SyncConnectionRevisions(ctx, map[string][]byte{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.ConnectionRevision(ctx, "photos"); err != nil || ok {
		t.Fatalf("deleted connection retained: ok=%v err=%v", ok, err)
	}
}

func TestRuleDefinitionChangeResetsDebounce(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err = s.PutRuleSince(ctx, "hot", "hash-a", now); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.RuleSince(ctx, "hot", "hash-a"); err != nil || !ok || !got.Equal(now) {
		t.Fatalf("got=%v ok=%v err=%v", got, ok, err)
	}
	if _, ok, err := s.RuleSince(ctx, "hot", "hash-b"); err != nil || ok {
		t.Fatalf("edited rule retained debounce: ok=%v err=%v", ok, err)
	}
}

func TestPruneRetentionTables(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	fresh := time.Now().UTC().Format(time.RFC3339Nano)
	for _, stamp := range []string{old, fresh} {
		_, err = s.DB().Exec(`INSERT INTO signal_history(card_id,signal,ts,value) VALUES(?,?,?,?)`, "c", "x", stamp, 1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.DB().Exec(`INSERT INTO events(ts,type,severity) VALUES(?,?,?)`, stamp, "x", "info")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Prune(ctx, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"signal_history", "events"} {
		var n int
		if err = s.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s rows=%d", table, n)
		}
	}
}

func TestConcurrentReadersDuringWrite(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	doc := widgets.Document{Blocks: []widgets.Block{}}
	cs := state.OK("card", doc, state.Source{}, time.Minute, time.Millisecond)
	if err = s.PutCard(ctx, storage.CardRecord{CardHash: "hash", State: cs}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, ok, readErr := s.GetCard(ctx, "card", "hash"); readErr != nil || !ok {
					t.Errorf("read ok=%v err=%v", ok, readErr)
					return
				}
			}
		}()
	}
	for range 20 {
		if err = s.PutCard(ctx, storage.CardRecord{CardHash: "hash", State: cs}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestCorruptDatabaseReportsClearError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/veduta.db", []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Open(context.Background(), dir); err == nil {
		t.Fatal("corrupt database opened successfully")
	}
}
