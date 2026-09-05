// SPDX-License-Identifier: AGPL-3.0-or-later

// Command veduta serves the dashboard.
//
//	veduta serve [--listen host:port]   run the HTTP server
//	veduta version [--json]             print build identity
//	veduta manifest digest <file>...    print the canonical digest of an integration manifest
//
// Until authentication lands (milestone H1) the server refuses to bind a non-loopback address,
// because it holds service credentials from Phase D onward. See docs/01-architecture.md D46.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"veduta.dev/veduta/internal/api"
	"veduta.dev/veduta/internal/canonical"
	"veduta.dev/veduta/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "veduta:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "version"
	if len(args) > 0 && !isFlag(args[0]) {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return serve(args)
	case "version":
		return printVersion(args)
	case "manifest":
		return manifestCmd(args)
	default:
		return fmt.Errorf("unknown command %q (try: serve, version, manifest)", cmd)
	}
}

func isFlag(s string) bool { return len(s) > 0 && s[0] == '-' }

// manifestCmd exposes the canonical digest, which is what veduta.lock.yaml records and what an
// approval is bound to. Having it in the CLI means an administrator can see exactly what they
// are approving, and that the lock file can be regenerated without guessing.
func manifestCmd(args []string) error {
	if len(args) == 0 || args[0] != "digest" {
		return errors.New("usage: veduta manifest digest <file>...")
	}
	files := args[1:]
	if len(files) == 0 {
		return errors.New("usage: veduta manifest digest <file>...")
	}
	for _, f := range files {
		d, err := canonical.DigestFile(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		fmt.Printf("%s  %s\n", d, f)
	}
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8099", "address to listen on (loopback until H1)")
	override := fs.Bool("i-know-what-im-doing", false,
		"allow a non-loopback bind before authentication exists")
	logFormat := fs.String("log-format", "text", "log format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var handler slog.Handler
	if *logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	logger := slog.New(handler)

	srv, err := api.New(api.Config{
		Listen:                 *listen,
		Logger:                 logger,
		AllowPublicWithoutAuth: *override,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func printVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print build identity as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := version.Current()
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}
	fmt.Printf("veduta %s (%s)\nsource: %s\n", info.Version, info.Commit, info.SourceURL)
	return nil
}
