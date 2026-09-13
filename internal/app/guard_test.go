// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/widgets"
)

// A panic in integration code must become an ordinary failed run. Declarative integrations
// evaluate in this process, so before the barrier existed a defect in a manifest's pipeline
// unwound past the scheduler's refresh goroutine and ended the program - which a container's
// restart policy turned into a crash loop on the same card, with nothing naming it.
func TestGuardedTurnsAPanicIntoAFailedRun(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	run := guarded(logger, "immich-recent", func(context.Context) (widgets.Document, error) {
		panic("assignment to entry in nil map")
	})

	doc, err := run(context.Background())
	if err == nil {
		t.Fatal("the panic did not become an error")
	}
	if !errors.Is(err, scheduler.ErrRunPanicked) {
		t.Fatalf("got %v, want scheduler.ErrRunPanicked", err)
	}
	if len(doc.Blocks) != 0 || doc.Title != "" {
		t.Fatalf("a panicked run returned a document: %#v", doc)
	}

	// The log is where the cause and the card go.
	out := logged.String()
	if !strings.Contains(out, "immich-recent") {
		t.Fatal("the log does not name the card that panicked")
	}
	if !strings.Contains(out, "assignment to entry in nil map") {
		t.Fatal("the log does not carry the panic value")
	}
	if !strings.Contains(out, "internal/app.TestGuardedTurnsAPanicIntoAFailedRun") {
		t.Fatalf("the log does not carry a usable stack:\n%s", out)
	}

	// And the card's visible text is where neither belongs: a panic value is arbitrary
	// in-process data, so it is described rather than rendered. Same reasoning as
	// scheduler.ErrSecretInDocument.
	if strings.Contains(err.Error(), "nil map") || strings.Contains(err.Error(), "internal/app.") {
		t.Fatalf("the card's error text leaks the panic value or a stack frame: %q", err.Error())
	}
}

func TestGuardedLeavesAnOrdinaryRunAlone(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	want := widgets.Document{Title: "Immich"}
	wantErr := errors.New("upstream refused")

	doc, err := guarded(logger, "card", func(context.Context) (widgets.Document, error) {
		return want, nil
	})(context.Background())
	if err != nil || doc.Title != "Immich" {
		t.Fatalf("doc=%#v err=%v", doc, err)
	}

	// An error the integration returns itself must reach the card unchanged: turning every
	// failure into ErrRunPanicked would hide the upstream reason the tile is meant to show.
	_, err = guarded(logger, "card", func(context.Context) (widgets.Document, error) {
		return widgets.Document{}, wantErr
	})(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want the run's own error", err)
	}
}

// The logger is only ever reached while already recovering, so a nil one would re-panic from
// inside the deferred function and lose the original cause.
func TestGuardedSurvivesANilLogger(t *testing.T) {
	_, err := guarded(nil, "card", func(context.Context) (widgets.Document, error) {
		panic("boom")
	})(context.Background())
	if !errors.Is(err, scheduler.ErrRunPanicked) {
		t.Fatalf("got %v", err)
	}
}
