// SPDX-License-Identifier: AGPL-3.0-or-later

// Package storage owns Veduta's SQLite persistence and forward-only migrations.
package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"veduta.dev/veduta/internal/state"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store owns Veduta's SQLite database and data directory.
type Store struct {
	db      *sql.DB
	dataDir string
}

// Open creates or opens a store and applies all pending migrations.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	// The DSN below is a file: URL, and url.URL renders a relative path as an authority rather
	// than a path - "data/veduta.db" becomes "file://data/veduta.db", which SQLite rejects with
	// "invalid uri authority: data", naming neither the setting nor the directory. Absolute
	// paths have no authority to mistake, so resolve before building the URL.
	path, err := filepath.Abs(filepath.Join(dataDir, "veduta.db"))
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	query := url.Values{}
	for _, pragma := range []string{"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(ON)", "synchronous(NORMAL)"} {
		query.Add("_pragma", pragma)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", "PRAGMA synchronous=NORMAL"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite %s: %w", pragma, err)
		}
	}
	s := &Store{db: db, dataDir: dataDir}
	if err = s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the underlying database for specialised package-level stores.
func (s *Store) DB() *sql.DB { return s.db }

// DataDir returns the directory that contains persistent Veduta data.
func (s *Store) DataDir() string { return s.dataDir }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var version int
		if _, err = fmt.Sscanf(e.Name(), "%d_", &version); err != nil {
			return fmt.Errorf("invalid migration name %s", e.Name())
		}
		var exists int
		if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`, version, time.Now().UTC().Format(time.RFC3339Nano))
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// DefinitionHash returns the stable SHA-256 identity of a JSON-marshalable definition.
func DefinitionHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256sum(b)
	return hex.EncodeToString(sum[:]), nil
}

func sha256sum(b []byte) [32]byte { // kept here so definition hashes share one stable implementation
	return sha256.Sum256(b)
}

// CardRecord combines rendered state with every input that controls its validity.
type CardRecord struct {
	CardHash, ManifestDigest, ApprovalRevision, SlotRevisions string
	State                                                     state.CardState
}

// PutCard validates and persists a card state.
func (s *Store) PutCard(ctx context.Context, r CardRecord) error {
	return s.PutCardWithSignals(ctx, r, nil)
}

// PutCardWithSignals atomically persists a card state and the numeric history samples produced
// by that same execution. A failed or superseded state commit can therefore never leave samples
// which do not correspond to a visible card execution.
func (s *Store) PutCardWithSignals(ctx context.Context, r CardRecord, signals map[string]float64) error {
	if err := r.State.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(r.State)
	if err != nil {
		return err
	}
	e := r.State.Execution
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO card_state(card_id,card_hash,manifest_digest,approval_revision,slot_revisions,envelope,state,generated_at,expires_at,next_run_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(card_id) DO UPDATE SET card_hash=excluded.card_hash,manifest_digest=excluded.manifest_digest,approval_revision=excluded.approval_revision,slot_revisions=excluded.slot_revisions,envelope=excluded.envelope,state=excluded.state,generated_at=excluded.generated_at,expires_at=excluded.expires_at,next_run_at=excluded.next_run_at,updated_at=excluded.updated_at`, r.State.CardID, r.CardHash, r.ManifestDigest, r.ApprovalRevision, r.SlotRevisions, b, e.State, nullString(e.GeneratedAt), nullString(e.ExpiresAt), nullString(e.NextRunAt), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	for name, value := range signals {
		if _, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO signal_history(card_id,signal,ts,value) VALUES(?,?,?,?)`, r.State.CardID, name, e.GeneratedAt, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetCard restores a card only when its configuration hash still matches.
func (s *Store) GetCard(ctx context.Context, id, cardHash string) (state.CardState, bool, error) {
	return s.GetCardFor(ctx, id, CardRecord{CardHash: cardHash})
}

// GetCardFor restores state only when every supplied execution-identity component still
// matches. This prevents a connection repoint, credential rotation, or approval change from
// briefly displaying data produced under the previous authority after restart/reload.
func (s *Store) GetCardFor(ctx context.Context, id string, expected CardRecord) (state.CardState, bool, error) {
	var storedHash, manifestDigest, approvalRevision, slotRevisions string
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT card_hash,coalesce(manifest_digest,''),coalesce(approval_revision,''),coalesce(slot_revisions,''),envelope FROM card_state WHERE card_id=?`, id).Scan(&storedHash, &manifestDigest, &approvalRevision, &slotRevisions, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return state.CardState{}, false, nil
	}
	if err != nil {
		return state.CardState{}, false, err
	}
	if storedHash != expected.CardHash ||
		(expected.ManifestDigest != "" && manifestDigest != expected.ManifestDigest) ||
		(expected.ApprovalRevision != "" && approvalRevision != expected.ApprovalRevision) ||
		(expected.SlotRevisions != "" && slotRevisions != expected.SlotRevisions) {
		if _, e := s.db.ExecContext(ctx, `DELETE FROM card_state WHERE card_id=?`, id); e != nil {
			return state.CardState{}, false, e
		}
		return state.CardState{}, false, nil
	}
	var out state.CardState
	if err = json.Unmarshal(b, &out); err != nil {
		return out, false, err
	}
	if err = out.Validate(); err != nil {
		return out, false, err
	}
	return out, true, nil
}

// Setting returns a persistent random byte setting, creating it when absent.
func (s *Store) Setting(ctx context.Context, key string, size int) ([]byte, error) {
	var value []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if err == nil {
		if len(value) != size {
			return nil, fmt.Errorf("setting %q has %d bytes, want %d", key, len(value), size)
		}
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	value = make([]byte, size)
	if _, err = rand.Read(value); err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, key, value, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return s.Setting(ctx, key, size)
}

// SyncConnectionRevisions atomically reconciles every configured connection against a private
// HMAC of its resolved material. The returned revisions are random public identifiers; no hash
// of configuration or credentials is ever exposed in a token.
func (s *Store) SyncConnectionRevisions(ctx context.Context, materialHMACs map[string][]byte) (map[string]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT connection_id,revision,material_hmac FROM connection_state`)
	if err != nil {
		return nil, err
	}
	type oldState struct {
		revision string
		material []byte
	}
	old := map[string]oldState{}
	for rows.Next() {
		var id, revision string
		var material []byte
		if err = rows.Scan(&id, &revision, &material); err != nil {
			_ = rows.Close()
			return nil, err
		}
		old[id] = oldState{revision, material}
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM connection_state`); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(materialHMACs))
	for id, material := range materialHMACs {
		revision := ""
		if previous, ok := old[id]; ok && len(previous.material) == len(material) && subtle.ConstantTimeCompare(previous.material, material) == 1 {
			revision = previous.revision
		}
		if revision == "" {
			random := make([]byte, 16)
			if _, err = rand.Read(random); err != nil {
				return nil, err
			}
			revision = hex.EncodeToString(random)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO connection_state(connection_id,revision,material_hmac,rotated_at) VALUES(?,?,?,?)`, id, revision, material, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return nil, err
		}
		out[id] = revision
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ConnectionRevision returns the current public revision for a connection.
func (s *Store) ConnectionRevision(ctx context.Context, id string) (string, bool, error) {
	var revision string
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM connection_state WHERE connection_id=?`, id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return revision, err == nil, err
}

// Prune removes retention-bound records older than before.
func (s *Store) Prune(ctx context.Context, before time.Time) error {
	for _, q := range []string{`DELETE FROM signal_history WHERE ts < ?`, `DELETE FROM events WHERE ts < ?`, `DELETE FROM plugin_kv WHERE expires_at IS NOT NULL AND expires_at < ?`} {
		if _, err := s.db.ExecContext(ctx, q, before.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}

// RunJanitor prunes retention-bound rows until ctx is cancelled.
func (s *Store) RunJanitor(ctx context.Context, retention, interval time.Duration, report func(error)) {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Prune(ctx, time.Now().Add(-retention)); err != nil && report != nil {
				report(err)
			}
		}
	}
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
