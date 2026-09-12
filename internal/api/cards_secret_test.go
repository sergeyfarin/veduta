// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"veduta.dev/veduta/internal/api"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/widgets"
)

// TestLeakingDocumentNeverReachesTheCardsAPI is the end-to-end half of the C2 acceptance test:
// the scheduler checks a produced document against the process-wide secret registry - the one
// every resolved secrets.Value registers itself with - so a plugin echoing a credential back
// into a card title cannot be served. It drives the real route, not the check in isolation.
func TestLeakingDocumentNeverReachesTheCardsAPI(t *testing.T) {
	const credential = "cards-api-must-never-serve-this-value"
	// Constructing a Value is how a resolved secret becomes known to the process; this is the
	// same registration production performs in secrets.ResolveAll.
	_ = secrets.New(credential)

	dir := t.TempDir()
	path := filepath.Join(dir, "veduta.yaml")
	body := []byte(`version: 1
auth: {mode: none}
integrations: [{id: demo, source: builtin}]
sections:
  - title: Test
    cards:
      - {id: leaky, title: Leaky, integration: demo, operation: show}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	store, diags := config.Open(path, nil, nil)
	if diags.HasErrors() {
		t.Fatal(diags.String())
	}

	manager := scheduler.New(nil)
	defer manager.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Apply(ctx, []scheduler.Definition{{ID: "leaky", Hash: "leaky", Refresh: time.Hour,
		Run: func(context.Context) (widgets.Document, error) {
			return widgets.Document{Title: "Leaky", Blocks: []widgets.Block{
				widgets.BlockText{Kind: widgets.TextPlain, Content: "x-api-key: " + credential},
			}}, nil
		}}}); err != nil {
		t.Fatal(err)
	}

	s, err := api.New(api.Config{Listen: "127.0.0.1:0", Assets: fstest.MapFS{}, ConfigStore: store, Scheduler: manager})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cards", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/cards = %d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), credential) {
			t.Fatalf("configured secret was served by /api/v1/cards: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), `"state":"error"`) {
			return // the run finished and was rejected, which is the state under test
		}
		if time.Now().After(deadline) {
			t.Fatalf("card never settled into an error state: %s", rec.Body.String())
		}
		time.Sleep(time.Millisecond)
	}
}
