// SPDX-License-Identifier: AGPL-3.0-or-later

// `veduta auth hash` - the missing first step of every non-loopback install. Password mode needs
// an Argon2id PHC string in auth.admin.passwordHash, and Veduta refuses to bind a non-loopback
// address without authentication, so an operator who cannot produce one cannot deploy. Before
// this existed the only route was the reference `argon2` CLI, an undocumented extra dependency on
// a deployment story whose whole point is a single static binary in a shell-less image.

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"veduta.dev/veduta/internal/auth"
)

func authCmd(args []string) error {
	usage := errors.New("usage: veduta auth hash [--memory KiB] [--iterations n] [--parallelism n]")
	if len(args) == 0 || args[0] != "hash" {
		return usage
	}
	fs := flag.NewFlagSet("auth hash", flag.ContinueOnError)
	cost := auth.DefaultHashCost
	memory := fs.Uint("memory", uint(cost.MemoryKiB), "Argon2id memory in KiB (8192-262144)")
	iterations := fs.Uint("iterations", uint(cost.Iterations), "Argon2id passes (1-10)")
	parallelism := fs.Uint("parallelism", uint(cost.Parallelism), "Argon2id lanes (1-16)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		// Deliberately not a positional argument: a password on the command line is visible in
		// the shell history of the person typing it and in `ps` output to everyone else.
		return errors.New("veduta auth hash reads the password from stdin; it is never an argument")
	}
	// The whole range is checked here, not just the ceilings the uint conversions below need, so
	// that an unusable cost is refused before the operator is asked for a password rather than
	// after. internal/auth checks it again; that copy is the authority, this one is the courtesy.
	if *memory < 8192 || *memory > 262144 || *iterations < 1 || *iterations > 10 || *parallelism < 1 || *parallelism > 16 {
		return errors.New("cost is outside the supported range: --memory 8192-262144, --iterations 1-10, --parallelism 1-16")
	}
	cost = auth.HashCost{MemoryKiB: uint32(*memory), Iterations: uint32(*iterations), Parallelism: uint8(*parallelism)}

	password, err := readPassword(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	encoded, err := auth.HashPassword(password, cost)
	if err != nil {
		return err
	}
	// stdout carries the hash and nothing else, so it pipes into a secret file or `docker secret
	// create` unedited. Every word of guidance above went to stderr.
	fmt.Println(encoded)
	return nil
}

// readPassword takes the password from the first line of in. Echo is not suppressed: doing that
// portably means a terminal-handling dependency, and CONTRIBUTING caps the direct Go module
// budget - so when in is a terminal this says plainly that the input will be visible, and the
// docs give the `read -rs` form for when that matters.
func readPassword(in *os.File, out io.Writer) (string, error) {
	if info, err := in.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		_, _ = fmt.Fprint(out, "Password (typed in the clear): ")
	}
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if password == "" {
		return "", errors.New("no password on stdin")
	}
	// A second line usually means the input was not what the operator thought it was - a file
	// holding more than the password, or a heredoc that picked up a stray line. Hashing the first
	// line silently would produce a verifier for a password nobody can reproduce.
	rest, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if strings.TrimSpace(string(rest)) != "" {
		return "", errors.New("stdin holds more than one line; the password must be the only content")
	}
	return password, nil
}
