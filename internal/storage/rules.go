// SPDX-License-Identifier: AGPL-3.0-or-later

package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RuleState is the persisted debounce and delivery state for one rule definition.
type RuleState struct {
	RuleID, RuleHash, LastResult         string
	Since, Deadline, FiredAt, ResolvedAt time.Time
	WrongType                            bool
}

// GetRuleState restores state only when its definition hash still matches.
func (s *Store) GetRuleState(ctx context.Context, id, hash string) (RuleState, bool, error) {
	var out RuleState
	var since, deadline, fired, resolved sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT rule_id,rule_hash,last_result,since,deadline_at,fired_at,resolved_at,wrong_type FROM rule_state WHERE rule_id=?`, id).Scan(&out.RuleID, &out.RuleHash, &out.LastResult, &since, &deadline, &fired, &resolved, &out.WrongType)
	if errors.Is(err, sql.ErrNoRows) {
		return RuleState{}, false, nil
	}
	if err != nil {
		return RuleState{}, false, err
	}
	if out.RuleHash != hash {
		return RuleState{}, false, nil
	}
	for source, target := range map[*sql.NullString]*time.Time{&since: &out.Since, &deadline: &out.Deadline, &fired: &out.FiredAt, &resolved: &out.ResolvedAt} {
		if source.Valid {
			*target, err = time.Parse(time.RFC3339Nano, source.String)
			if err != nil {
				return RuleState{}, false, err
			}
		}
	}
	return out, true, nil
}

// PutRuleState stores one rule's complete state.
func (s *Store) PutRuleState(ctx context.Context, value RuleState) error {
	_, err := putRuleState(ctx, s.db, value)
	return err
}

// CommitRuleTransition atomically persists one evaluated rule state, its optional user-visible
// event, every notification outbox insertion, and notification flood-suspension meta-events.
// It returns true when at least one deliverable outbox row was inserted.
func (s *Store) CommitRuleTransition(ctx context.Context, value RuleState, event *Event, notifications []NotificationRequest) (bool, error) {
	if event != nil {
		copy := *event
		if err := validateEvent(&copy); err != nil {
			return false, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	wake := false
	for _, notification := range notifications {
		enqueued, _, enqueueErr := enqueueNotificationTx(ctx, tx, notification)
		if enqueueErr != nil {
			return false, enqueueErr
		}
		wake = wake || enqueued
	}
	if event != nil {
		if err = appendEventTx(ctx, tx, *event); err != nil {
			return false, err
		}
	}
	if _, err = putRuleState(ctx, tx, value); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return wake, nil
}

type ruleStateExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func putRuleState(ctx context.Context, executor ruleStateExecutor, value RuleState) (sql.Result, error) {
	return executor.ExecContext(ctx, `INSERT INTO rule_state(rule_id,rule_hash,since,deadline_at,last_result,fired_at,resolved_at,wrong_type) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(rule_id) DO UPDATE SET rule_hash=excluded.rule_hash,since=excluded.since,deadline_at=excluded.deadline_at,last_result=excluded.last_result,fired_at=excluded.fired_at,resolved_at=excluded.resolved_at,wrong_type=excluded.wrong_type`, value.RuleID, value.RuleHash, nullTime(value.Since), nullTime(value.Deadline), value.LastResult, nullTime(value.FiredAt), nullTime(value.ResolvedAt), value.WrongType)
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
