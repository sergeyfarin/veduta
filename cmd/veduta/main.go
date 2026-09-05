// SPDX-License-Identifier: AGPL-3.0-or-later

// Command veduta serves the dashboard.
//
//	veduta serve [--listen host:port]   run the HTTP server
//	veduta version [--json]             print build identity
//	veduta manifest digest <file>...    print the canonical digest of an integration manifest
//	veduta --check-config [--config path]   validate a config file and print diagnostics
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
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/fixtures"
	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "veduta:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// --check-config is a flag, not a subcommand, per its own usage line above - checked first
	// so the isFlag/subcommand split below never has to know about it.
	if len(args) > 0 && args[0] == "--check-config" {
		return checkConfig(args[1:])
	}
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

// checkConfig is milestone C1's own acceptance criterion: human-readable diagnostics, a non-zero
// exit on error. It exits directly rather than returning an error through run/main, because
// main's own error path prints a single "veduta: <err>" line - the wrong shape for what is
// usually several independent diagnostics, one per file:line:col.
func checkConfig(args []string) error {
	fs := flag.NewFlagSet("--check-config", flag.ContinueOnError)
	path := fs.String("config", "veduta.yaml",
		"path to the primary config file; a sibling conf.d/*.yaml is loaded automatically")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, diags := config.LoadPath(*path)
	if len(diags) > 0 {
		fmt.Fprintln(os.Stderr, diags.String())
	}
	if diags.HasErrors() {
		os.Exit(1)
	}
	fmt.Println("config OK:", *path)
	return nil
}

func isFlag(s string) bool { return len(s) > 0 && s[0] == '-' }

// manifestCmd exposes the canonical digest, which is what veduta.lock.yaml records and what an
// approval is bound to. Having it in the CLI means an administrator can see exactly what they
// are approving, and that the lock file can be regenerated without guessing.
func manifestCmd(args []string) error {
	usage := errors.New("usage: veduta manifest digest <file> [file...]")
	if len(args) == 0 || args[0] != "digest" {
		return usage
	}
	files := args[1:]
	if len(files) == 0 {
		return usage
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
	listen := fs.String("listen", "", "address to listen on (overrides server.listen in config)")
	configPath := fs.String("config", "veduta.yaml", "path to the primary config file")
	override := fs.Bool("i-know-what-im-doing", false,
		"allow a non-loopback bind before authentication exists")
	logFormat := fs.String("log-format", "text", "log format: text or json")
	serveFixtures := fs.Bool("fixtures", false,
		"serve the checked-in showcase dashboard instead of real configuration (dev only)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var handler slog.Handler
	if *logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	// Scrubbing wraps every log line by construction, not by remembering to redact at each call
	// site - defence in depth (docs/01-architecture.md section 2), active from the first log line
	// even before any config exists to resolve a secret from.
	logger := slog.New(secrets.NewHandler(handler, secrets.DefaultRegistry()))

	cfg := api.Config{
		Logger:                 logger,
		AllowPublicWithoutAuth: *override,
	}
	if *serveFixtures {
		bundle, err := fixtures.Load()
		if err != nil {
			return fmt.Errorf("--fixtures: %w", err)
		}
		cfg.Fixtures = &bundle
		cfg.Listen = *listen
		logger.Warn("serving the checked-in showcase dashboard, not real configuration",
			"hint", "this is --fixtures - remove it for a real deployment")
	} else {
		loader := func(path string) (*config.Snapshot, config.Diagnostics) {
			snapshot, diags := config.LoadPath(path)
			if snapshot == nil || diags.HasErrors() {
				return nil, diags
			}
			_, secretDiags := secrets.ResolveAll(snapshot.SecretRefs, secrets.DefaultResolver())
			diags = append(diags, secretDiags...)
			if diags.HasErrors() {
				return nil, diags
			}
			return snapshot, diags
		}
		store, diags := config.Open(*configPath, logger, loader)
		if diags.HasErrors() {
			return fmt.Errorf("load config:\n%s", diags.String())
		}
		if err := validateAuthNone(store.Snapshot(), *override); err != nil {
			return err
		}
		cfg.ConfigStore = store
		cfg.Listen = store.Snapshot().Config.Server.Listen
		if *listen != "" {
			cfg.Listen = *listen
		}
	}

	srv, err := api.New(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.ConfigStore != nil {
		go func() {
			if err := cfg.ConfigStore.Watch(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("config watcher stopped", "error", err)
				stop()
			}
		}()
	}
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func validateAuthNone(snapshot *config.Snapshot, override bool) error {
	if snapshot.Config.Auth.Mode == config.AuthNone && len(snapshot.SecretRefs) > 0 && !override {
		return errors.New("auth.mode is none but the configuration references secrets; pass --i-know-what-im-doing to acknowledge the risk")
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
