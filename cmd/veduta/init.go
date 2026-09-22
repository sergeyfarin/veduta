// SPDX-License-Identifier: AGPL-3.0-or-later

// `veduta init` - the first run, done for you. It writes a minimal veduta.yaml with an admin
// password already hashed into it, so that `docker compose up -d` is the whole installation
// rather than the last of three manual steps.
//
// It is deliberately idempotent: an existing configuration is left exactly as it is and the
// command succeeds, because it runs on every container start as a one-shot service and must not
// rewrite a configuration the operator has since edited.

package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"veduta.dev/veduta/internal/auth"
)

// containerUID is the uid the image runs as. `init` only ever grants this uid group access to
// the configuration directory, and only when it is running as root and asked to - see
// fixOwnership.
const containerUID = 65532

// passwordAlphabet excludes the characters that are misread when a password is copied out of a
// terminal by eye: 0/O, 1/l/I. What remains is 32 symbols, so each character carries exactly 5
// bits and generatePassword's entropy is a multiplication rather than an estimate.
const passwordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// passwordLength puts a generated password at 100 bits of entropy (20 x 5). That is far past
// what an online login can be brute-forced at, and the point is that nobody should feel the need
// to replace it with something memorable.
const passwordLength = 20

func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", "/config/veduta.yaml", "configuration file to create")
	listen := fs.String("listen", "0.0.0.0:8099", "server.listen to write")
	dataDir := fs.String("data-dir", "/data", "server.dataDir to write")
	title := fs.String("title", "Home", "dashboard.title to write")
	username := fs.String("username", "admin", "administrator username to write")
	fixPerms := fs.Bool("fix-permissions", false,
		"when running as root, give uid 65532 group access to the configuration directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: veduta init [--config path] [--listen addr] [--data-dir path]")
	}
	return runInit(os.Stdout, os.Stderr, *configPath, *listen, *dataDir, *title, *username, *fixPerms)
}

func runInit(stdout, stderr io.Writer, configPath, listen, dataDir, title, username string, fixPerms bool) error {
	dir := filepath.Dir(configPath)
	// Ownership is fixed before the existence check, so that a container restarting against a
	// configuration written by an earlier version still ends up with a writable directory - the
	// lock file needs one whether or not this run creates veduta.yaml.
	if fixPerms {
		if err := fixOwnership(stderr, dir); err != nil {
			return err
		}
	}
	if _, err := os.Stat(configPath); err == nil {
		_, _ = fmt.Fprintf(stderr, "veduta init: %s already exists, leaving it alone\n", configPath)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", configPath, err)
	}

	password, err := generatePassword()
	if err != nil {
		return fmt.Errorf("generating a password: %w", err)
	}
	// An operator-supplied password is honoured so that a scripted deployment can know the
	// credential in advance. It is hashed here and the plaintext is never written anywhere:
	// what lands in veduta.yaml is a verifier either way.
	generated := true
	if supplied := os.Getenv("VEDUTA_ADMIN_PASSWORD"); supplied != "" {
		password, generated = supplied, false
	}
	hash, err := auth.HashPassword(password, auth.DefaultHashCost)
	if err != nil {
		return fmt.Errorf("hashing the password: %w", err)
	}

	if err = os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err = writeConfig(configPath, listen, dataDir, title, username, hash); err != nil {
		return err
	}
	// Written by root in the container case, into a directory the operator owns on the host.
	// Handing it back is what keeps `$EDITOR config/veduta.yaml` working without sudo, and the
	// group is what lets uid 65532 read it at all - without both, the server cannot start.
	if fixPerms {
		if err = shareWithContainer(dir, configPath); err != nil {
			return err
		}
	}
	announce(stdout, stderr, configPath, username, password, generated)
	return nil
}

// writeConfig writes veduta.yaml atomically and mode 0640: it holds a password verifier, so it
// is not world-readable, and the group is what lets uid 65532 read it when the directory has
// been prepared by fixOwnership. O_EXCL on the temporary file, and a rename onto the target,
// mean two containers racing to initialise the same directory cannot interleave a half-written
// configuration - the loser's rename simply wins or loses whole.
func writeConfig(configPath, listen, dataDir, title, username, hash string) error {
	var b strings.Builder
	b.WriteString("# Written by `veduta init`. Edit freely - Veduta reloads this file when it changes.\n")
	b.WriteString("# The configuration reference is docs/configuration.md; examples/veduta.yaml is a\n")
	b.WriteString("# worked configuration with connections and cards to borrow from.\n")
	b.WriteString("version: 1\n\nserver:\n")
	fmt.Fprintf(&b, "  listen: %q\n", listen)
	fmt.Fprintf(&b, "  dataDir: %s\n", dataDir)
	b.WriteString("\nauth:\n  mode: password\n  admin:\n")
	fmt.Fprintf(&b, "    username: %s\n", username)
	// The hash is quoted because a PHC string starts with '$' and contains ',' and '=' - all
	// harmless inside double quotes, and all capable of surprising a reader who edits by hand.
	fmt.Fprintf(&b, "    passwordHash: %q\n", hash)
	b.WriteString("\ndashboard:\n")
	fmt.Fprintf(&b, "  title: %s\n", title)
	b.WriteString("\nsections: []\n")

	dir := filepath.Dir(configPath)
	tmp, err := os.CreateTemp(dir, ".veduta.yaml.*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w (is the directory writable by this user?)", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err = tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err = tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting permissions on %s: %w", tmpName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, configPath); err != nil {
		return fmt.Errorf("installing %s: %w", configPath, err)
	}
	return nil
}

// fixOwnership is the one thing that cannot be done from inside the hardened container, and the
// reason compose runs `init` as root in a one-shot service: a bind-mounted ./config belongs to
// the operator on the host, and uid 65532 cannot write into it. Rather than transfer the
// directory - which would take `sudo` to edit veduta.yaml afterwards - it leaves the owner alone
// and grants the group, so the operator keeps editing and the container can write its lock file.
//
// A directory the container uid can already write is left untouched, which is the named-volume
// case: the image ships /config owned by 65532, so there is nothing to fix.
func fixOwnership(stderr io.Writer, dir string) error {
	if os.Geteuid() != 0 {
		// Not an error: outside a container `init` runs as whoever owns the directory, and
		// there is nothing to grant. Saying so beats failing on a flag compose always passes.
		_, _ = fmt.Fprintln(stderr, "veduta init: --fix-permissions needs root, skipping (not an error outside a container)")
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	var err error
	// -1 leaves the uid alone: the host operator stays the owner of their own directory.
	if err = os.Chown(dir, -1, containerUID); err != nil {
		return fmt.Errorf("granting group %d access to %s: %w", containerUID, dir, err)
	}
	// #nosec G302 -- 0770 is the point: the operator owns the directory and the server's uid is
	// its group, and both must be able to create files in it. It is not world-accessible.
	if err = os.Chmod(dir, 0o770); err != nil {
		return fmt.Errorf("making %s group-writable: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, entry := range entries {
		// Repairs anything an earlier run left owned by root, which the operator could not edit
		// and, if its group went with it, the server could not read.
		if err = shareWithContainer(dir, filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	// Docker creates a missing bind-mount source itself, owned by root. Preserving that owner is
	// correct - init must not guess who the operator is - but it leaves them unable to edit their
	// own configuration, so it is worth saying plainly rather than leaving as a discovery.
	if info, err := os.Stat(dir); err == nil {
		if uid, ok := ownerUID(info); ok && uid == 0 {
			// The chown has to happen on the host, against the directory mounted here, whose
			// path this process cannot know - so the hint names it the way compose does.
			_, _ = fmt.Fprintf(stderr, "veduta init: %s is owned by root, which usually means Docker "+
				"created it because it did not exist.\n"+
				"            The server can use it, but editing veduta.yaml will take sudo. To take it\n"+
				"            back, on the host, against the directory mounted here:\n"+
				"              sudo chown -R $(id -u) ./config\n", dir)
		}
	}
	_, _ = fmt.Fprintf(stderr, "veduta init: %s is now group %d and group-writable; its owner is unchanged\n",
		dir, containerUID)
	return nil
}

// shareWithContainer makes path readable and writable by both sides: owned by whoever owns dir -
// the operator, on a bind mount - and grouped to the uid the server runs as. Either half alone
// is a broken deployment. Without the uid, a root-owned veduta.yaml takes sudo to edit; without
// the group, mode 0640 hides it from the server, which then crash-loops on `permission denied`.
func shareWithContainer(dir, path string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspecting %s: %w", dir, err)
	}
	uid, ok := ownerUID(info)
	if !ok {
		return nil
	}
	if err = os.Chown(path, uid, containerUID); err != nil {
		return fmt.Errorf("sharing %s between uid %d and group %d: %w", path, uid, containerUID, err)
	}
	return nil
}

// announce writes the whole block to one stream. Splitting the password onto stdout reads well
// in a terminal and badly everywhere else: `docker compose logs` merges the two streams without
// ordering them, so the password surfaced below its own explanation, after the closing advice.
func announce(_, stderr io.Writer, configPath, username, password string, generated bool) {
	_, _ = fmt.Fprintf(stderr, "veduta init: wrote %s\n", configPath)
	if !generated {
		_, _ = fmt.Fprintf(stderr, "veduta init: hashed the password from VEDUTA_ADMIN_PASSWORD for user %q\n", username)
		return
	}
	// Shown exactly once: only the verifier is stored, so nothing can print it again.
	_, _ = fmt.Fprintf(stderr, "\n  Veduta is configured. Sign in as %q with this password:\n\n"+
		"      %s\n\n"+
		"  It is shown once and stored only as a hash. Save it now.\n"+
		"  Change it by replacing auth.admin.passwordHash with `veduta auth hash`.\n\n", username, password)
}

// generatePassword draws from crypto/rand with a rejection-free alphabet: 32 symbols divides 256
// evenly, so indexing a uniform byte into it stays uniform and no modulo bias creeps in.
func generatePassword() (string, error) {
	buf := make([]byte, passwordLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, passwordLength)
	for i, b := range buf {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out), nil
}
