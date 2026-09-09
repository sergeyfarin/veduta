// SPDX-License-Identifier: AGPL-3.0-or-later

// Package audit persists security-relevant operator and runtime events.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"veduta.dev/veduta/internal/storage"
)

// Entry is one immutable audit record.
type Entry struct {
	Actor   string
	IP      string
	Action  string
	Target  string
	Outcome string
	Detail  any
}

// Log appends records to the persistent audit_log table.
type Log struct {
	store *storage.Store
	now   func() time.Time
}

// New creates a persistent audit log.
func New(store *storage.Store) (*Log, error) {
	if store == nil {
		return nil, errors.New("audit: store is required")
	}
	return &Log{store: store, now: time.Now}, nil
}

// Record appends an entry after serialising its bounded structured detail.
func (l *Log) Record(ctx context.Context, entry Entry) error {
	if entry.Action == "" || entry.Outcome == "" {
		return errors.New("audit: action and outcome are required")
	}
	var detail []byte
	var err error
	if entry.Detail != nil {
		detail, err = json.Marshal(entry.Detail)
		if err != nil {
			return err
		}
		if len(detail) > 64<<10 {
			return errors.New("audit: detail exceeds 64 KiB")
		}
	}
	_, err = l.store.DB().ExecContext(ctx, `INSERT INTO audit_log(ts,actor,ip,action,target,outcome,detail) VALUES(?,?,?,?,?,?,?)`, l.now().UTC().Format(time.RFC3339Nano), nullIfEmpty(entry.Actor), nullIfEmpty(entry.IP), entry.Action, nullIfEmpty(entry.Target), entry.Outcome, detail)
	return err
}

// Denied implements capabilities.Audit for broker policy denials.
func (l *Log) Denied(pluginID, method string, cause error) {
	_ = l.Record(context.Background(), Entry{Action: "capability.denied", Target: pluginID, Outcome: "failure", Detail: map[string]string{"method": method, "error": cause.Error()}})
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
