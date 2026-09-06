// SPDX-License-Identifier: AGPL-3.0-or-later

package integrations

import (
	"context"
	"encoding/json"
	"time"

	"veduta.dev/veduta/internal/capabilities"
	"veduta.dev/veduta/internal/integrations/manifestload"
	"veduta.dev/veduta/internal/widgets"
)

// Runtime loads one approved integration runtime implementation.
type Runtime interface {
	Name() string
	Load(context.Context, Installed) (Instance, error)
}

// Instance is a loaded integration with immutable compiled operations.
type Instance interface {
	Operations() []OperationSpec
	Invoke(context.Context, InvokeRequest) (InvokeResponse, error)
	Close(context.Context) error
}

// Installed pairs an executable manifest with its approval record.
type Installed struct {
	Manifest *manifestload.Manifest
	Lock     *LockEntry
}

// InvokeRequest contains one operation invocation and its fenced authority.
type InvokeRequest struct {
	Operation string
	Params    json.RawMessage
	Grant     capabilities.Grant
	Deadline  time.Time
}

// InvokeResponse is a validated widget document plus non-fatal diagnostics.
type InvokeResponse struct {
	Document widgets.Document
	Diags    []Diagnostic
}

// OperationSpec describes an operation without exposing runtime internals.
type OperationSpec struct {
	ID, Name, DefaultRefresh string
	Signals                  []manifestload.SignalDecl
}

// Diagnostic is a non-fatal runtime message.
type Diagnostic struct{ Severity, Message string }
