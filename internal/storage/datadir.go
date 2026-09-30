// SPDX-License-Identifier: AGPL-3.0-or-later

package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// CheckDataDir reports whether dir can hold Veduta's database and caches, and returns it resolved
// to an absolute path - the path the operator needs to see, since a relative server.dataDir is
// resolved against the process's working directory, not the configuration file's. An empty dir
// means Open's own default, "data".
//
// It exists because the failures it names used to surface from SQLite, naming neither the setting
// nor the path: a relative value once produced "invalid uri authority: data", and a directory the
// process cannot write produces "unable to open database file". Callers prefix the error with
// where the value came from (server.dataDir in a file, or --data-dir).
//
// With create, a missing directory is created, as Open would; without it - for a check that must
// not change anything - a missing directory passes only if its nearest existing ancestor is one
// this process could create it in.
func CheckDataDir(dir string, create bool) (string, error) {
	if dir == "" {
		dir = "data"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir, fmt.Errorf("resolving %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	// A path through a file ("data.db/sub") is ENOTDIR, not "does not exist"; either way the
	// question is which ancestor exists, so both take the walk below rather than MkdirAll's
	// message, which names the operation instead of the component that is in the way.
	missing := errors.Is(err, os.ErrNotExist)
	throughFile := errors.Is(err, syscall.ENOTDIR)
	switch {
	case err == nil && !info.IsDir():
		return abs, fmt.Errorf("%s is a file, not a directory", abs)
	case err == nil:
		// exists; checked for writability below
	case missing && create:
		if err = os.MkdirAll(abs, 0o700); err != nil {
			return abs, fmt.Errorf("%s does not exist and cannot be created: %w", abs, err)
		}
	case missing || throughFile:
		parent := filepath.Dir(abs)
		for {
			if pinfo, perr := os.Stat(parent); perr == nil {
				if !pinfo.IsDir() {
					return abs, fmt.Errorf("%s cannot be created: %s is a file", abs, parent)
				}
				if werr := writable(parent); werr != nil {
					return abs, fmt.Errorf("%s does not exist and cannot be created in %s: %w", abs, parent, werr)
				}
				return abs, nil
			}
			next := filepath.Dir(parent)
			if next == parent {
				return abs, fmt.Errorf("%s: no existing parent directory", abs)
			}
			parent = next
		}
	default:
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err // the path is already in the message once
		}
		return abs, fmt.Errorf("%s: %w", abs, err)
	}
	if err = writable(abs); err != nil {
		return abs, fmt.Errorf("%s is not writable by this process (uid %d): %w", abs, os.Getuid(), err)
	}
	return abs, nil
}

// writable creates and removes a file in dir, which is the only reliable answer: permission bits
// say nothing about read-only mounts, ACLs or a container's user namespace.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".veduta-write-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
