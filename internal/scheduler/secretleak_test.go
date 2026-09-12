// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/widgets"
)

const leakedSecret = "leakedcredential123"

func registryWithLeakedSecret() *secrets.Registry {
	reg := secrets.NewRegistry()
	reg.Track(leakedSecret)
	return reg
}

// TestProducedDocumentCarryingASecretIsRejected is the production wiring the C2 acceptance test
// ("a secret value appearing in a Widget Document is rejected") always described and nothing
// enforced: an integration never receives a credential, so a verbatim match means a resolved
// secret came back through the data path. The document must be dropped rather than committed.
func TestProducedDocumentCarryingASecretIsRejected(t *testing.T) {
	m := scheduler.NewWithSecrets(nil, registryWithLeakedSecret())
	defer m.Close()
	definition := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Title: "Jellyfin", Blocks: []widgets.Block{
			widgets.BlockText{Kind: widgets.TextPlain, Content: "authorization: Bearer " + leakedSecret},
		}}, nil
	}}
	if err := m.Apply(context.Background(), []scheduler.Definition{definition}); err != nil {
		t.Fatal(err)
	}
	card := waitForState(t, m, "card", state.StateError)
	if card.Document != nil {
		t.Fatalf("leaking document was committed: %+v", card.Document)
	}
	if card.Execution.Error == nil || card.Execution.Error.Code != state.ErrorInvalid {
		t.Fatalf("error=%+v, want code %q", card.Execution.Error, state.ErrorInvalid)
	}
	// The error text is a card's visible message: it must name the problem without repeating it.
	if encoded, err := json.Marshal(card); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(encoded), leakedSecret) {
		t.Fatalf("secret reached the served card state: %s", encoded)
	}
}

// TestCleanDocumentIsUnaffected pins that the check costs a correct integration nothing - a
// document that merely mentions the same card is committed as usual.
func TestCleanDocumentIsUnaffected(t *testing.T) {
	m := scheduler.NewWithSecrets(nil, registryWithLeakedSecret())
	defer m.Close()
	definition := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, Run: func(context.Context) (widgets.Document, error) {
		return widgets.Document{Title: "Jellyfin", Blocks: []widgets.Block{}}, nil
	}}
	if err := m.Apply(context.Background(), []scheduler.Definition{definition}); err != nil {
		t.Fatal(err)
	}
	if card := waitForState(t, m, "card", state.StateOK); card.Document == nil {
		t.Fatal("clean document was not committed")
	}
}

// TestRestoredDocumentCarryingASecretIsNotServed covers the other way a document reaches a
// viewer: one persisted before the matching secret was configured, restored by Apply on the next
// start. It must not come back; the card starts pending and refreshes instead.
func TestRestoredDocumentCarryingASecretIsNotServed(t *testing.T) {
	store, err := storage.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	block := widgets.BlockText{Kind: widgets.TextPlain, Content: "authorization: Bearer " + leakedSecret}
	run := func(context.Context) (widgets.Document, error) {
		return widgets.Document{Title: "Jellyfin", Blocks: []widgets.Block{block}}, nil
	}
	definition := scheduler.Definition{ID: "card", Hash: "card", Refresh: time.Hour, Run: run}

	// First start: the value is not a secret yet, so the document is stored.
	before := scheduler.NewWithSecrets(store, secrets.NewRegistry())
	if err := before.Apply(context.Background(), []scheduler.Definition{definition}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, before, "card", state.StateOK)
	before.Close()

	// Second start: the same value is now a configured secret.
	blocked := make(chan struct{})
	definition.Run = func(ctx context.Context) (widgets.Document, error) {
		<-blocked // hold the refresh open so only the restored state can be observed
		return widgets.Document{Blocks: []widgets.Block{}}, nil
	}
	after := scheduler.NewWithSecrets(store, registryWithLeakedSecret())
	defer after.Close()
	defer close(blocked)
	if err := after.Apply(context.Background(), []scheduler.Definition{definition}); err != nil {
		t.Fatal(err)
	}
	card, ok := after.State("card")
	if !ok {
		t.Fatal("card missing after Apply")
	}
	if card.Document != nil {
		t.Fatalf("stored leaking document was restored: %+v", card.Document)
	}
	if card.Execution.State != state.StatePending {
		t.Fatalf("state=%s, want %s", card.Execution.State, state.StatePending)
	}
}
