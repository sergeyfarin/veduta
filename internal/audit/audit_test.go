// SPDX-License-Identifier: AGPL-3.0-or-later

package audit

import (
	"context"
	"testing"

	"veduta.dev/veduta/internal/storage"
)

func TestRecordPersistsAttributionAndDetail(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	log, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Record(context.Background(), Entry{Actor: "admin", IP: "192.0.2.1", Action: "integration.approve", Target: "jellyfin", Outcome: "success", Detail: map[string]string{"digest": "abc"}}); err != nil {
		t.Fatal(err)
	}
	var actor, action, detail string
	if err := store.DB().QueryRow(`SELECT actor,action,detail FROM audit_log`).Scan(&actor, &action, &detail); err != nil {
		t.Fatal(err)
	}
	if actor != "admin" || action != "integration.approve" || detail != `{"digest":"abc"}` {
		t.Fatalf("actor=%q action=%q detail=%s", actor, action, detail)
	}
}
