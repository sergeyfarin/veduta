// Package contracts' tests are the project's contract suite. They replace the Python checker
// that existed before there was a Go module, and they run four layers:
//
//	structural  - every schema is valid, and every fixture and adversarial case lands on its
//	              expected side (testdata/schema-cases.json)
//	portability - every `pattern` compiles under Go's regexp (RE2). Compiling the schemas at all
//	              proves this, and TestPatternsCompile reports which pattern fails if one does.
//	canonical   - RFC 8785 digests over golden fixtures, including the real YAML manifests, with
//	              must-match and must-differ invariants
//	semantic    - cross-document checks, run against the real examples AND against every
//	              checked-in negative fixture, so deleting a check fails the build
//
// Note on `format`: date-time and uri stay ANNOTATIONS (AssertFormat is deliberately not called).
// A pattern is not a parser either, so semantics are enforced with real parsers - see
// checker.scanValues, NormaliseMediaType and ValidSemver.
package contracts_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"veduta.dev/veduta/internal/canonical"
	"veduta.dev/veduta/internal/contracts"
)

var schemaFiles = map[string]string{
	"widget":    "widget-document.v1.schema.json",
	"cardstate": "card-state.v1.schema.json",
	"manifest":  "plugin-manifest.v1.schema.json",
	"config":    "config.v1.schema.json",
	"lock":      "integration-lock.v1.schema.json",
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the module root")
		}
		dir = parent
	}
}

func loadJSON(t *testing.T, path string) any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

// loadYAML decodes YAML into the shape the JSON Schema validator expects, rejecting duplicate
// mapping keys on the way through - YAML accepts them silently, and they are a smuggling vector
// for approval diffs.
func loadYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	doc, err := canonical.Load(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	normalised, err := toJSONShape(doc)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	m, ok := normalised.(map[string]any)
	if !ok {
		t.Fatalf("%s: expected a mapping at the top level", path)
	}
	return m
}

// toJSONShape converts decoded YAML into the value shapes the validator and the semantic checks
// expect (numbers as float64), without going through a JSON round trip.
func toJSONShape(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			n, err := toJSONShape(e)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			n, err := toJSONShape(e)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case uint64:
		return float64(t), nil
	default:
		return v, nil
	}
}

type compiled map[string]*jsonschema.Schema

func compileSchemas(t *testing.T) compiled {
	t.Helper()
	root := repoRoot(t)
	c := jsonschema.NewCompiler()
	ids := map[string]string{}
	for name, file := range schemaFiles {
		doc := loadJSON(t, filepath.Join(root, "schemas", file))
		id, _ := doc.(map[string]any)["$id"].(string)
		if id == "" {
			t.Fatalf("%s has no $id", file)
		}
		if err := c.AddResource(id, doc); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		ids[name] = id
	}
	// AssertFormat is deliberately NOT called: format stays an annotation (see the package doc).
	out := compiled{}
	for name, id := range ids {
		sch, err := c.Compile(id)
		if err != nil {
			t.Fatalf("compiling %s: %v", name, err)
		}
		out[name] = sch
	}
	return out
}

// ---------------------------------------------------------------- structural

func TestSchemasCompile(t *testing.T) {
	if got := len(compileSchemas(t)); got != len(schemaFiles) {
		t.Fatalf("compiled %d schemas, want %d", got, len(schemaFiles))
	}
}

func TestFixturesValidate(t *testing.T) {
	root := repoRoot(t)
	schemas := compileSchemas(t)
	fixtures := []struct{ path, schema string }{
		{"examples/veduta.yaml", "config"},
		{"examples/veduta.lock.yaml", "lock"},
		{"plugins/immich/manifest.yaml", "manifest"},
		{"plugins/glances/manifest.yaml", "manifest"},
		{"plugins/jellyfin/manifest.yaml", "manifest"},
		{"testdata/widgets/jellyfin-recent.golden.json", "widget"},
		{"testdata/widgets/jellyfin-recent.cardstate.json", "cardstate"},
	}
	for _, f := range fixtures {
		t.Run(f.path, func(t *testing.T) {
			p := filepath.Join(root, f.path)
			var doc any
			if strings.HasSuffix(f.path, ".yaml") {
				doc = loadYAML(t, p)
			} else {
				doc = loadJSON(t, p)
			}
			if err := schemas[f.schema].Validate(doc); err != nil {
				t.Errorf("must validate against %s: %v", f.schema, err)
			}
		})
	}
}

type schemaCase struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
	Expect string `json:"expect"`
	Doc    any    `json:"doc"`
}

func TestSchemaCorpus(t *testing.T) {
	root := repoRoot(t)
	schemas := compileSchemas(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata/schema-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []schemaCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) < 50 {
		t.Fatalf("corpus shrank to %d cases; it is the contract, not a sample", len(corpus.Cases))
	}
	for _, tc := range corpus.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			sch, ok := schemas[tc.Schema]
			if !ok {
				t.Fatalf("unknown schema %q", tc.Schema)
			}
			err := sch.Validate(tc.Doc)
			switch tc.Expect {
			case "accept":
				if err != nil {
					t.Errorf("expected accept, got reject: %v", err)
				}
			case "reject":
				if err == nil {
					t.Error("expected reject, got accept")
				}
			default:
				t.Fatalf("unknown expectation %q", tc.Expect)
			}
		})
	}
}

// ---------------------------------------------------------------- portability

// TestPatternsCompile checks every `pattern` against Go's regexp. The schemas would fail to
// compile anyway, but this reports WHICH pattern is at fault - the first draft used
// `(?!.*\.\.)`, which RE2 rejects outright and which would have stopped the server at boot.
func TestPatternsCompile(t *testing.T) {
	root := repoRoot(t)
	total := 0
	for name, file := range schemaFiles {
		doc := loadJSON(t, filepath.Join(root, "schemas", file))
		for path, pat := range collectPatterns(doc, "") {
			total++
			if _, err := regexp.Compile(pat); err != nil {
				t.Errorf("%s%s: RE2 cannot compile %q: %v", name, path, pat, err)
			}
			if strings.Contains(pat, "(?!") || strings.Contains(pat, "(?=") {
				t.Errorf("%s%s: pattern uses lookahead, which RE2 does not support: %q", name, path, pat)
			}
		}
	}
	if total < 40 {
		t.Fatalf("only found %d patterns; the walk is probably broken", total)
	}
}

func collectPatterns(node any, path string) map[string]string {
	out := map[string]string{}
	switch t := node.(type) {
	case map[string]any:
		for k, v := range t {
			if k == "pattern" {
				if s, ok := v.(string); ok {
					out[path+"/pattern"] = s
				}
			}
			for p, s := range collectPatterns(v, path+"/"+k) {
				out[p] = s
			}
		}
	case []any:
		for i, v := range t {
			for p, s := range collectPatterns(v, fmt.Sprintf("%s/%d", path, i)) {
				out[p] = s
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- canonical digest

func TestCanonicalDigests(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata/canonical/expected-digests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Digests    map[string]string `json:"digests"`
		Manifests  map[string]string `json:"manifests"`
		MustMatch  [][]string        `json:"must_match"`
		MustDiffer [][]string        `json:"must_differ"`
	}
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, name := range sortedMapKeys(expected.Digests) {
		d, err := canonical.DigestFile(filepath.Join(root, "testdata/canonical", name+".json"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got[name] = d
		if d != expected.Digests[name] {
			t.Errorf("digest %s = %s, want %s", name, d, expected.Digests[name])
		}
	}
	// The real manifests go through the same strict YAML decoder the loader will use.
	for _, name := range sortedMapKeys(expected.Manifests) {
		d, err := canonical.DigestFile(filepath.Join(root, "plugins", name, "manifest.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got[name] = d
		if d != expected.Manifests[name] {
			t.Errorf("manifest digest %s = %s, want %s", name, d, expected.Manifests[name])
		}
	}
	for _, pair := range expected.MustMatch {
		if got[pair[0]] != got[pair[1]] {
			t.Errorf("%s and %s must normalise to the same digest", pair[0], pair[1])
		}
	}
	for _, pair := range expected.MustDiffer {
		if got[pair[0]] == got[pair[1]] {
			t.Errorf("%s and %s must NOT collide: normalisation is path-aware, so arbitrary user "+
				"data named 'capabilities' or 'queryKeys' must be left alone", pair[0], pair[1])
		}
	}
}

func TestCanonicalStrictness(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct{ name, file, body, want string }{
		{"two JSON documents", "two.json", `{"a":1}{"b":2}`, "trailing content"},
		{"two YAML documents", "two.yaml", "a: 1\n---\nb: 2\n", "exactly one YAML document"},
		{"duplicate mapping key", "dup.yaml", "a: 1\na: 2\n", "duplicate mapping key"},
		{"float", "float.json", `{"a":1.5}`, "floating-point"},
		{"integer beyond 2^53", "big.json", `{"a":9007199254740992}`, "safe range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := canonical.DigestFile(write(tc.file, tc.body))
			if err == nil {
				t.Fatalf("expected a rejection mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- helpers

func TestValidSemver(t *testing.T) {
	cases := map[string]bool{
		"1.0.0": true, "1.2.3-rc.1": true, "1.0.0-0.3.7": true, "1.0.0+b.1": true,
		"1.0.0-01": false, "1.0.0-alpha..1": false, "01.0.0": false, "1.0.0+": false,
		"1.0.0-": false, "1.0": false, "": false,
	}
	for v, want := range cases {
		if got := contracts.ValidSemver(v); got != want {
			t.Errorf("ValidSemver(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestNormaliseMediaType(t *testing.T) {
	cases := map[string]string{
		"application/json":                       "application/json",
		"Application/JSON; Charset=UTF-8":        "application/json; charset=utf-8",
		`application/vnd.api+json;profile="a;b"`: `application/vnd.api+json; profile="a;b"`,
		"application/json;b=2;a=1":               "application/json; a=1; b=2",
	}
	for in, want := range cases {
		got, err := contracts.NormaliseMediaType(in)
		if err != nil {
			t.Errorf("NormaliseMediaType(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormaliseMediaType(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := contracts.NormaliseMediaType("nonsense/"); err == nil {
		t.Error("expected an invalid media type to be rejected")
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/assets/*/thumbnail", "/api/assets/abc/thumbnail", true},
		{"/api/assets/*/thumbnail", "/api/assets/a/b/thumbnail", false}, // * never crosses /
		{"/Users/*/Items", "/Users/1/Items", true},
		{"/api/4/cpu", "/api/4/cpu", true},
		{"/api/4/cpu", "/api/4/mem", false},
	}
	for _, tc := range cases {
		if got := contracts.GlobMatch(tc.pattern, tc.path); got != tc.want {
			t.Errorf("GlobMatch(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestCodeSpansIgnoreStringLiterals(t *testing.T) {
	expr := `state("a") == "signal(ghost, x)"`
	var code strings.Builder
	for _, s := range contracts.CodeSpans(expr) {
		code.WriteString(expr[s[0]:s[1]])
	}
	if strings.Contains(code.String(), "signal(") {
		t.Errorf("call-like text inside a string literal leaked into the code spans: %q", code.String())
	}
}

var _ = yaml.Unmarshal
