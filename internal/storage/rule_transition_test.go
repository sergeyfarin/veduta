// SPDX-License-Identifier: AGPL-3.0-or-later

package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"veduta.dev/veduta/internal/storage"
)

func TestCommitRuleTransitionRollsBackEveryCrashBoundary(t *testing.T) {
	for _, eventType := range []string{"rule.fired", "rule.resolved"} {
		t.Run(eventType, func(t *testing.T) {
			store, err := storage.Open(context.Background(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			now := time.Now().UTC()
			// Force the last statement in the transaction to fail. The request reaches the hourly
			// ceiling first, so rollback must remove its channel state and suspension meta-event as
			// well as the transition event.
			_, err = store.DB().Exec(`INSERT INTO notifications(created_at,channel,dedupe_key,payload,state,next_attempt_at) VALUES(?,?,?,?,?,?)`, now.Format(time.RFC3339Nano), "phone", "old", []byte(`{}`), "sent", now.Format(time.RFC3339Nano))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.DB().Exec(`CREATE TRIGGER reject_rule_state BEFORE INSERT ON rule_state BEGIN SELECT RAISE(FAIL, 'injected crash boundary'); END`); err != nil {
				t.Fatal(err)
			}
			state := storage.RuleState{RuleID: "hot", RuleHash: "hash", LastResult: "true", FiredAt: now}
			event := storage.Event{Timestamp: now.Format(time.RFC3339Nano), Type: eventType, Severity: "critical", Source: "rules", Data: []byte(`{"rule":"hot"}`)}
			request := storage.NotificationRequest{Channel: "phone", DedupeKey: "phone:hot:fired", Payload: []byte(`{"ruleId":"hot"}`), Now: now, PerHour: 1}
			if _, err = store.CommitRuleTransition(context.Background(), state, &event, []storage.NotificationRequest{request}); err == nil {
				t.Fatal("injected transaction failure was ignored")
			}
			assertCount(t, store, "rule_state", 0)
			assertCount(t, store, "events", 0)
			assertCount(t, store, "notification_channel_state", 0)
			assertCount(t, store, "notifications", 1)
		})
	}
}

func TestCommitRuleTransitionCommitsStateEventAndOutboxTogether(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	state := storage.RuleState{RuleID: "hot", RuleHash: "hash", LastResult: "true", FiredAt: now}
	event := storage.Event{Timestamp: now.Format(time.RFC3339Nano), Type: "rule.fired", Severity: "critical", Source: "rules", Data: []byte(`{"rule":"hot"}`)}
	request := storage.NotificationRequest{Channel: "phone", DedupeKey: "phone:hot:fired", Payload: []byte(`{"ruleId":"hot"}`), Now: now, PerHour: 20}
	wake, err := store.CommitRuleTransition(context.Background(), state, &event, []storage.NotificationRequest{request})
	if err != nil {
		t.Fatal(err)
	}
	if !wake {
		t.Fatal("committed outbox insertion did not request a dispatcher wake-up")
	}
	assertCount(t, store, "rule_state", 1)
	assertCount(t, store, "events", 1)
	assertCount(t, store, "notifications", 1)
}

func assertCount(t *testing.T, store *storage.Store, table string, want int) {
	t.Helper()
	var got int
	if err := store.DB().QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
