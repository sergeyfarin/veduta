// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestIntegrationImportBoundary keeps runtime code away from persistence and credential
// implementations. The two connection imports below are narrow, reviewed core adapters: Docker
// consumes the public DockerRegistry interface, while routepath is the shared canonicaliser every
// permission check must use. Any new exception requires an explicit review here.
func TestIntegrationImportBoundary(t *testing.T) {
	root := repoRoot(t)
	integrationsDir := filepath.Join(root, "internal", "integrations")
	allowedConnections := map[string]map[string]bool{
		filepath.Join("internal", "integrations", "docker", "docker.go"): {
			"veduta.dev/veduta/internal/connections": true,
		},
		filepath.Join("internal", "integrations", "route.go"): {
			"veduta.dev/veduta/internal/connections/routepath": true,
		},
		filepath.Join("internal", "integrations", "manifestload", "httpjson.go"): {
			"veduta.dev/veduta/internal/connections/routepath": true,
		},
		filepath.Join("internal", "integrations", "manifestload", "load.go"): {
			"veduta.dev/veduta/internal/connections/routepath": true,
		},
	}
	files := 0
	err := filepath.WalkDir(integrationsDir, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(filename, ".go") || strings.HasSuffix(filename, "_test.go") {
			return walkErr
		}
		files++
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, filename)
		for _, declaration := range parsed.Decls {
			imports, ok := declaration.(*ast.GenDecl)
			if !ok || imports.Tok != token.IMPORT {
				continue
			}
			for _, spec := range imports.Specs {
				path, err := strconv.Unquote(spec.(*ast.ImportSpec).Path.Value)
				if err != nil {
					return err
				}
				switch {
				case path == "veduta.dev/veduta/internal/storage" || strings.HasPrefix(path, "veduta.dev/veduta/internal/storage/"),
					path == "veduta.dev/veduta/internal/secrets" || strings.HasPrefix(path, "veduta.dev/veduta/internal/secrets/"):
					t.Errorf("%s imports forbidden core implementation %s", relative, path)
				case path == "veduta.dev/veduta/internal/connections" || strings.HasPrefix(path, "veduta.dev/veduta/internal/connections/"):
					if !allowedConnections[relative][path] {
						t.Errorf("%s adds unreviewed connection-layer dependency %s", relative, path)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("scanned zero integration source files")
	}
}
