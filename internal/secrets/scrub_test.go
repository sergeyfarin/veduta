// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"veduta.dev/veduta/internal/secrets"
)

func TestRegistry_ScrubReplacesKnownValues(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("mysecretvalue123") // long enough to be tracked
	got := reg.Scrub("connecting with token mysecretvalue123 to host x")
	if strings.Contains(got, "mysecretvalue123") {
		t.Fatalf("Scrub did not redact: %q", got)
	}
}

// TestRegistry_ShortValuesAreNotTracked: docs/01-architecture.md section 2's own stated rule -
// secrets under 8 characters are excluded to avoid false-positive redactions.
func TestRegistry_ShortValuesAreNotTracked(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("short12") // 7 chars
	got := reg.Scrub("the value short12 appears here")
	if !strings.Contains(got, "short12") {
		t.Fatal("a 7-character value should not be tracked for scrubbing")
	}
}

func TestRegistry_ContainsSecret(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("apikey1234567890")
	if !reg.ContainsSecret("document text with apikey1234567890 embedded") {
		t.Fatal("want ContainsSecret to find the tracked value")
	}
	if reg.ContainsSecret("document text with nothing sensitive") {
		t.Fatal("want ContainsSecret to be false when no tracked value is present")
	}
}

// TestNew_RegistersWithDefaultRegistry: every Value ever constructed via New is tracked for
// scrubbing immediately, whether or not Reveal is ever called on it - the whole point of
// defence-in-depth starting at resolution time, not at first use.
func TestNew_RegistersWithDefaultRegistry(t *testing.T) {
	const distinctive = "distinctivetestvalue987654"
	secrets.New(distinctive)
	if !secrets.DefaultRegistry().ContainsSecret(distinctive) {
		t.Fatal("New should register its value with the default registry")
	}
}

// recordingHandler captures every record handed to it, for assertions on what actually reached
// "the next handler" after scrubbing.
type recordingHandler struct {
	buf *bytes.Buffer
	h   slog.Handler
}

func newRecordingJSONHandler() *recordingHandler {
	buf := &bytes.Buffer{}
	return &recordingHandler{buf: buf, h: slog.NewJSONHandler(buf, nil)}
}

func TestScrubbingHandler_Message(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("leakyvalue1234567")
	rh := newRecordingJSONHandler()
	logger := slog.New(secrets.NewHandler(rh.h, reg))

	logger.Info("token is leakyvalue1234567 during connect")

	out := rh.buf.String()
	if strings.Contains(out, "leakyvalue1234567") {
		t.Fatalf("message leaked through: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Fatalf("expected a redaction marker in output: %s", out)
	}
}

func TestScrubbingHandler_StringAttr(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("attrsecretvalue123")
	rh := newRecordingJSONHandler()
	logger := slog.New(secrets.NewHandler(rh.h, reg))

	logger.Info("connecting", "token", "attrsecretvalue123")

	out := rh.buf.String()
	if strings.Contains(out, "attrsecretvalue123") {
		t.Fatalf("attribute leaked through: %s", out)
	}
}

func TestScrubbingHandler_GroupAttr(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("groupedsecretvalue1")
	rh := newRecordingJSONHandler()
	logger := slog.New(secrets.NewHandler(rh.h, reg))

	logger.Info("connecting", slog.Group("conn", "token", "groupedsecretvalue1"))

	out := rh.buf.String()
	if strings.Contains(out, "groupedsecretvalue1") {
		t.Fatalf("grouped attribute leaked through: %s", out)
	}
}

func TestScrubbingHandler_WithAttrs(t *testing.T) {
	reg := secrets.NewRegistry()
	reg.Track("withattrssecret1234")
	rh := newRecordingJSONHandler()
	logger := slog.New(secrets.NewHandler(rh.h, reg)).With("token", "withattrssecret1234")

	logger.Info("connecting")

	out := rh.buf.String()
	if strings.Contains(out, "withattrssecret1234") {
		t.Fatalf("attribute added via With leaked through: %s", out)
	}
}

func TestScrubbingHandler_NonSecretTextPassesThroughUnchanged(t *testing.T) {
	reg := secrets.NewRegistry()
	rh := newRecordingJSONHandler()
	logger := slog.New(secrets.NewHandler(rh.h, reg))

	logger.Info("listening", "addr", "127.0.0.1:8099")

	out := rh.buf.String()
	if !strings.Contains(out, "127.0.0.1:8099") {
		t.Fatalf("ordinary log content should pass through unchanged: %s", out)
	}
}
