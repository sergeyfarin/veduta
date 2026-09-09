// SPDX-License-Identifier: AGPL-3.0-or-later

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Event is one persistent, user-visible runtime occurrence.
type Event struct {
	ID        int64           `json:"id"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Severity  string          `json:"severity"`
	Source    string          `json:"source,omitempty"`
	CardID    string          `json:"cardId,omitempty"`
	Message   string          `json:"message,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// AppendEvent stores a bounded structured event.
func (s *Store) AppendEvent(ctx context.Context, event Event) error {
	if err := validateEvent(&event); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO events(ts,type,severity,source,card_id,message,data) VALUES(?,?,?,?,?,?,?)`, event.Timestamp, event.Type, event.Severity, nullString(event.Source), nullString(event.CardID), nullString(event.Message), event.Data)
	return err
}

func validateEvent(event *Event) error {
	if event.Type == "" || event.Severity == "" {
		return errors.New("storage: event type and severity are required")
	}
	if len(event.Data) > 64<<10 || len(event.Message) > 4096 {
		return errors.New("storage: event exceeds size limit")
	}
	if len(event.Data) > 0 && !json.Valid(event.Data) {
		return errors.New("storage: event data is not valid JSON")
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return nil
}

func appendEventTx(ctx context.Context, tx *sql.Tx, event Event) error {
	if err := validateEvent(&event); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO events(ts,type,severity,source,card_id,message,data) VALUES(?,?,?,?,?,?,?)`, event.Timestamp, event.Type, event.Severity, nullString(event.Source), nullString(event.CardID), nullString(event.Message), event.Data)
	return err
}

// RecentEvents returns newest-first events with a server-side maximum.
func (s *Store) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,ts,type,severity,coalesce(source,''),coalesce(card_id,''),coalesce(message,''),coalesce(data,'') FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var data []byte
		if err := rows.Scan(&event.ID, &event.Timestamp, &event.Type, &event.Severity, &event.Source, &event.CardID, &event.Message, &data); err != nil {
			return nil, err
		}
		if len(data) > 0 {
			event.Data = json.RawMessage(data)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// PutSignalHistory stores one coherent set of declared numeric samples.
func (s *Store) PutSignalHistory(ctx context.Context, cardID, timestamp string, values map[string]float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for name, value := range values {
		if _, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO signal_history(card_id,signal,ts,value) VALUES(?,?,?,?)`, cardID, name, timestamp, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}
