// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manifestload parses and statically validates executable integration manifests.
package manifestload

import "gopkg.in/yaml.v3"

// Manifest is the fully validated executable view of a plugin manifest.
type Manifest struct {
	Path, Digest, ID, Name, Version, Runtime string
	Module, ModuleSHA256                     string
	Capabilities                             []string
	Slots                                    []SlotSpec
	Limits                                   Limits
	Operations                               []OperationDef
}

// SlotSpec declares one abstract connection slot.
type SlotSpec struct {
	Name, Kind string
	Required   bool
}

// SignalDecl declares the name and scalar shape of one emitted signal.
type SignalDecl struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Unit    string `yaml:"unit"`
	History bool   `yaml:"history"`
}

// Route is an operation's requested upstream authority.
type Route struct {
	Slot        string   `yaml:"slot"`
	Method      string   `yaml:"method"`
	Path        string   `yaml:"path"`
	Use         string   `yaml:"use"`
	ContentType string   `yaml:"contentType"`
	QueryKeys   []string `yaml:"queryKeys"`
	MaxBodyKB   int      `yaml:"maxBodyKB"`
}

// Limits contains effective declarative runtime ceilings.
type Limits struct {
	MemoryMB      int `yaml:"memoryMB"`
	TimeoutMs     int `yaml:"timeoutMs"`
	OutputKB      int `yaml:"outputKB"`
	HTTPRequests  int `yaml:"httpRequests"`
	ResponseMB    int `yaml:"responseMB"`
	CacheEntries  int `yaml:"cacheEntries"`
	InputMB       int `yaml:"inputMB"`
	JSONDepth     int `yaml:"jsonDepth"`
	JSONNodes     int `yaml:"jsonNodes"`
	ExprNodes     int `yaml:"exprNodes"`
	Iterations    int `yaml:"iterations"`
	RequestBodyKB int `yaml:"requestBodyKB"`
	HostCalls     int `yaml:"hostCalls"`
	CacheBytesKB  int `yaml:"cacheBytesKB"`
}

// OperationDef is one compiled-ready declarative operation.
type OperationDef struct {
	ID, Name, DefaultRefresh string
	Params                   any
	Routes                   []Route
	Signals                  []SignalDecl
	Pipeline                 []PipelineStep
	Output                   *Template
}

// PipelineStep binds one broker response into the expression environment.
type PipelineStep struct {
	As      string
	When    *Expression
	Request RequestDef
}

// RequestDef is a recursively compiled broker request template.
type RequestDef struct {
	Slot, Method   string
	Path           *Template
	Query, Headers map[string]*Template
	Body           *BodyDef
}

// BodyDef is either a structured JSON body or form fields.
type BodyDef struct {
	JSON *Template
	Form map[string]*Template
}

// Expression retains source location and validated AST complexity.
type Expression struct {
	Source              string
	Line, Column, Nodes int
}

// AssetDef describes a broker-minted asset reference.
type AssetDef struct {
	Slot      string
	Path      *Template
	Query     map[string]*Template
	Transform string
}

// EachDef expands its item template once per collection element.
type EachDef struct {
	Expr *Expression
	As   string
	Item *Template
}

// Template is one node in the closed declarative output grammar.
type Template struct {
	Kind    string // literal, object, array, expr, asset, each
	Literal any
	Object  map[string]*Template
	Array   []*Template
	Expr    *Expression
	Asset   *AssetDef
	Each    *EachDef
	Node    *yaml.Node
}

// WithDefaults fills omitted limits with the schema defaults.
func (l Limits) WithDefaults() Limits {
	defaults := Limits{64, 3000, 64, 8, 4, 64, 4, 32, 200000, 512, 20000, 64, 256, 256}
	if l.MemoryMB == 0 {
		l.MemoryMB = defaults.MemoryMB
	}
	if l.TimeoutMs == 0 {
		l.TimeoutMs = defaults.TimeoutMs
	}
	if l.OutputKB == 0 {
		l.OutputKB = defaults.OutputKB
	}
	if l.HTTPRequests == 0 {
		l.HTTPRequests = defaults.HTTPRequests
	}
	if l.ResponseMB == 0 {
		l.ResponseMB = defaults.ResponseMB
	}
	if l.CacheEntries == 0 {
		l.CacheEntries = defaults.CacheEntries
	}
	if l.InputMB == 0 {
		l.InputMB = defaults.InputMB
	}
	if l.JSONDepth == 0 {
		l.JSONDepth = defaults.JSONDepth
	}
	if l.JSONNodes == 0 {
		l.JSONNodes = defaults.JSONNodes
	}
	if l.ExprNodes == 0 {
		l.ExprNodes = defaults.ExprNodes
	}
	if l.Iterations == 0 {
		l.Iterations = defaults.Iterations
	}
	if l.RequestBodyKB == 0 {
		l.RequestBodyKB = defaults.RequestBodyKB
	}
	if l.HostCalls == 0 {
		l.HostCalls = defaults.HostCalls
	}
	if l.CacheBytesKB == 0 {
		l.CacheBytesKB = defaults.CacheBytesKB
	}
	return l
}
