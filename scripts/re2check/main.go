// Compiles every `pattern` in schemas/*.json with Go's regexp (RE2), which is what
// santhosh-tekuri/jsonschema uses. A pattern that only compiles in Python or in a
// JavaScript engine would make the Go validator refuse the schema at startup.
//
// Usage: go run ./scripts/re2check schemas/*.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
)

func patterns(v any, path string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "pattern" {
				if s, ok := t[k].(string); ok {
					out[path+"/"+k] = s
				}
			}
			if k == "propertyNames" {
				if m, ok := t[k].(map[string]any); ok {
					if s, ok := m["pattern"].(string); ok {
						out[path+"/propertyNames/pattern"] = s
					}
				}
			}
			patterns(t[k], path+"/"+k, out)
		}
	case []any:
		for i, e := range t {
			patterns(e, fmt.Sprintf("%s/%d", path, i), out)
		}
	}
}

func main() {
	fail, total := 0, 0
	for _, f := range os.Args[1:] {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Printf("FAIL  %s: %v\n", f, err)
			fail++
			continue
		}
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			fmt.Printf("FAIL  %s: %v\n", f, err)
			fail++
			continue
		}
		found := map[string]string{}
		patterns(doc, "", found)
		paths := make([]string, 0, len(found))
		for p := range found {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			total++
			if _, err := regexp.Compile(found[p]); err != nil {
				fmt.Printf("FAIL  %s%s: RE2 cannot compile %q: %v\n", f, p, found[p], err)
				fail++
			}
		}
	}
	fmt.Printf("re2check: %d patterns, %d failures\n", total, fail)
	if fail > 0 {
		os.Exit(1)
	}
}
