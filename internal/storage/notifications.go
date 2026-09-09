// SPDX-License-Identifier: AGPL-3.0-or-later

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Notification is one credential-free outbox item.
type Notification struct {
	ID       int64
	Channel  string
	Payload  []byte
	Attempts int
}

// EnqueueNotification inserts an item unless its cooldown dedupe key is recent or the channel's
// hourly ceiling is reached. newlySuspended is true exactly once per suspension window.
func (s *Store) EnqueueNotification(ctx context.Context, channel, dedupeKey string, payload []byte, now time.Time, cooldown time.Duration, perHour int) (enqueued, newlySuspended bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var suspended sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT suspended_until FROM notification_channel_state WHERE channel=?`, channel).Scan(&suspended)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, false, err
	}
	if suspended.Valid {
		until, parseErr := time.Parse(time.RFC3339Nano, suspended.String)
		if parseErr != nil {
			return false, false, parseErr
		}
		if now.Before(until) {
			return false, false, tx.Commit()
		}
	}
	if cooldown > 0 && dedupeKey != "" {
		var count int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE dedupe_key=? AND (state IN ('pending','sending') OR created_at>=?)`, dedupeKey, now.Add(-cooldown).UTC().Format(time.RFC3339Nano)).Scan(&count)
		if err != nil || count > 0 {
			return false, false, err
		}
	}
	if perHour <= 0 {
		perHour = 20
	}
	var hourly int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE channel=? AND created_at>=?`, channel, now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)).Scan(&hourly); err != nil {
		return false, false, err
	}
	if hourly >= perHour {
		until := now.Add(time.Hour).UTC().Format(time.RFC3339Nano)
		if _, err = tx.ExecContext(ctx, `INSERT INTO notification_channel_state(channel,suspended_until,suspension_reported_at) VALUES(?,?,?) ON CONFLICT(channel) DO UPDATE SET suspended_until=excluded.suspended_until,suspension_reported_at=excluded.suspension_reported_at`, channel, until, now.UTC().Format(time.RFC3339Nano)); err != nil {
			return false, false, err
		}
		data, marshalErr := json.Marshal(map[string]string{"channel": channel})
		if marshalErr != nil {
			return false, false, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(ts,type,severity,source,message,data) VALUES(?,?,?,?,?,?)`, now.UTC().Format(time.RFC3339Nano), "notification.suspended", "warning", "notifications", "Notification channel suspended after reaching its hourly ceiling", data); err != nil {
			return false, false, err
		}
		return false, true, tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notifications(created_at,channel,dedupe_key,payload,state,next_attempt_at) VALUES(?,?,?,?,?,?)`, now.UTC().Format(time.RFC3339Nano), channel, nullString(dedupeKey), payload, "pending", now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, false, err
	}
	return inserted == 1, false, tx.Commit()
}

// ResetSendingNotifications makes crash-interrupted claims eligible again.
func (s *Store) ResetSendingNotifications(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET state='pending' WHERE state='sending'`)
	return err
}

// ClaimNotification atomically claims the oldest due outbox item.
func (s *Store) ClaimNotification(ctx context.Context, now time.Time) (Notification, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Notification{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var out Notification
	err = tx.QueryRowContext(ctx, `SELECT id,channel,payload,attempts FROM notifications WHERE state='pending' AND (next_attempt_at IS NULL OR next_attempt_at<=?) ORDER BY id LIMIT 1`, now.UTC().Format(time.RFC3339Nano)).Scan(&out.ID, &out.Channel, &out.Payload, &out.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return Notification{}, false, tx.Commit()
	}
	if err != nil {
		return Notification{}, false, err
	}
	out.Attempts++
	result, err := tx.ExecContext(ctx, `UPDATE notifications SET state='sending',attempts=? WHERE id=? AND state='pending'`, out.Attempts, out.ID)
	if err != nil {
		return Notification{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return Notification{}, false, err
	}
	return out, true, tx.Commit()
}

// CompleteNotification marks a successful delivery.
func (s *Store) CompleteNotification(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET state='sent',next_attempt_at=NULL,last_error=NULL WHERE id=?`, id)
	return err
}

// FailNotification retries at next or permanently abandons the item.
func (s *Store) FailNotification(ctx context.Context, id int64, next time.Time, message string, abandon bool) error {
	state := "pending"
	var nextValue any = next.UTC().Format(time.RFC3339Nano)
	if abandon {
		state, nextValue = "abandoned", nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET state=?,next_attempt_at=?,last_error=? WHERE id=?`, state, nextValue, message, id)
	return err
}
