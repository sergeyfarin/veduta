// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// safeSecretName is deliberately the same character class internal/config.secretRefPattern
// captures ([A-Za-z0-9_]+), checked again here rather than trusted from the caller: FileProvider
// joins name onto a directory to build a path, and a provider must not depend on every future
// caller having already validated its input the same way config's own decoder happens to.
var safeSecretName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Provider resolves one secret's value from one source. ok=false with err=nil means "this
// provider has nothing for name, try the next one" - only a real problem (a file that exists but
// cannot be read) is an error, since treating "not configured here" as an error would break the
// whole point of trying providers in order. The interface, not a concrete pair of functions, is
// deliberate: docs/01-architecture.md section 2 notes an encrypted-at-rest provider is a later
// (0.3) concern, and Provider already allows adding one without changing any caller.
type Provider interface {
	Resolve(name string) (value string, ok bool, err error)
}

// EnvProvider resolves NAME from the process environment.
type EnvProvider struct{}

// Resolve implements Provider.
func (EnvProvider) Resolve(name string) (string, bool, error) {
	v, ok := os.LookupEnv(name)
	return v, ok, nil
}

// FileProvider resolves NAME from a file at Dir/NAME, trimmed - the Docker secrets convention
// (Dir defaults to /run/secrets via DefaultResolver). Trimming matters because a secret file is
// commonly created with a trailing newline (`echo secret > file` versus `printf secret > file`),
// which would otherwise become part of the value.
type FileProvider struct {
	Dir string
}

// Resolve implements Provider.
func (p FileProvider) Resolve(name string) (string, bool, error) {
	if !safeSecretName.MatchString(name) {
		return "", false, fmt.Errorf("secret name %q contains characters other than letters, digits and underscore", name)
	}
	// #nosec G304 -- name is checked against safeSecretName immediately above (no '/', no '..',
	// no path separators of any kind can reach here); Dir is operator-configured, not
	// attacker-controlled request input.
	data, err := os.ReadFile(filepath.Join(p.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}
