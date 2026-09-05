// SPDX-License-Identifier: AGPL-3.0-or-later

package connections

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestNewHTTPClient_InsecureSkipVerifyLogsWarning is the D1 AC: "InsecureSkipVerify requires
// explicit config and logs a warning." The "requires explicit config" half is already true by
// construction - TLSConfig.InsecureSkipVerify only becomes true if a config author wrote it, the
// zero value is false - so this proves the other half: turning it on is never silent.
func TestNewHTTPClient_InsecureSkipVerifyLogsWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	_, err := newHTTPClient("x", &HTTPConfig{
		BaseURL: "https://example.com",
		TLS:     TLSConfig{InsecureSkipVerify: true},
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "insecure") && !strings.Contains(strings.ToLower(buf.String()), "tls") {
		t.Fatalf("expected a warning about disabled TLS verification, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("expected a WARN-level log line, got: %s", buf.String())
	}
}

func TestNewHTTPClient_NoWarningWhenVerificationEnabled(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	_, err := newHTTPClient("x", &HTTPConfig{BaseURL: "https://example.com"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("no warning expected when TLS verification is left enabled, got: %s", buf.String())
	}
}

func TestNewHTTPClient_RejectsNonHTTPScheme(t *testing.T) {
	_, err := newHTTPClient("x", &HTTPConfig{BaseURL: "ftp://example.com"}, testLogger())
	if err == nil {
		t.Fatal("want an error for a non-http(s) baseUrl")
	}
}

func TestNewHTTPClient_RejectsInvalidCAFile(t *testing.T) {
	_, err := newHTTPClient("x", &HTTPConfig{
		BaseURL: "https://example.com",
		TLS:     TLSConfig{CAFile: "/does/not/exist"},
	}, testLogger())
	if err == nil {
		t.Fatal("want an error for a missing CA file")
	}
}
