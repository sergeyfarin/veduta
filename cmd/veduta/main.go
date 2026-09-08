// SPDX-License-Identifier: AGPL-3.0-or-later

// Command veduta serves the dashboard.
//
//	veduta serve [--listen host:port]   run the HTTP server
//	veduta version [--json]             print build identity
//	veduta manifest digest <file>...    print the canonical digest of an integration manifest
//	veduta integration list             show every declared integration's lock status
//	veduta integration diff <id>        print the permission diff since the last approval
//	veduta integration approve <id>     review the diff and record approval in veduta.lock.yaml
//	veduta plugin validate <file.wasm>   run sandbox and ABI conformance checks
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
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"veduta.dev/veduta/internal/api"
	appcore "veduta.dev/veduta/internal/app"
	"veduta.dev/veduta/internal/canonical"
	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/fixtures"
	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/secrets"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/storage/assetcache"
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
	case "integration":
		return integrationCmd(args)
	case "plugin":
		return pluginCmd(args)
	default:
		return fmt.Errorf("unknown command %q (try: serve, version, manifest, integration, plugin)", cmd)
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
	dataDir := fs.String("data-dir", "", "directory for Veduta's persistent database and caches (overrides server.dataDir)")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		store, diags := config.Open(*configPath, logger, configLoader(*override))
		if diags.HasErrors() {
			return fmt.Errorf("load config:\n%s", diags.String())
		}
		cfg.ConfigStore = store
		cfg.Listen = store.Snapshot().Config.Server.Listen
		if *listen != "" {
			cfg.Listen = *listen
		}

		snapshot := store.Snapshot()
		dir := snapshot.Config.Server.DataDir
		if *dataDir != "" {
			dir = *dataDir
		}
		db, err := storage.Open(ctx, dir)
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}
		defer func() { _ = db.Close() }()
		go db.RunJanitor(ctx, 30*24*time.Hour, time.Hour, func(err error) {
			logger.Error("storage janitor failed", "error", err)
		})
		manager := scheduler.New(db)
		instanceKey, err := db.Setting(ctx, "instance-key", 32)
		if err != nil {
			return fmt.Errorf("load instance key: %w", err)
		}
		signingKey, err := db.Setting(ctx, "asset-signing-key", 32)
		if err != nil {
			return fmt.Errorf("load asset signing key: %w", err)
		}
		tokens, err := assettokens.New(signingKey)
		if err != nil {
			return err
		}
		registry, _, runtimeGeneration, err := buildRuntime(ctx, snapshot, store.Status().Generation, *configPath, db, instanceKey, tokens, logger)
		if err != nil {
			return fmt.Errorf("build runtime: %w", err)
		}
		if err = manager.Apply(ctx, runtimeGeneration.Definitions); err != nil {
			closeRuntimeGeneration(runtimeGeneration, logger)
			return fmt.Errorf("start schedules: %w", err)
		}
		runtimes := &runtimeHolder{current: runtimeGeneration}
		var reloads sync.WaitGroup
		defer func() {
			stop()
			manager.Close()
			reloads.Wait()
			runtimes.Close(logger)
		}()
		dynamic := connections.NewDynamic(registry)
		cfg.Registry = dynamic
		cfg.Scheduler = manager
		assetCache, err := assetcache.New(db, 512<<20)
		if err != nil {
			return fmt.Errorf("open asset cache: %w", err)
		}
		cfg.AssetProxy = &api.AssetProxy{Tokens: tokens, Store: db, Cache: assetCache, Registry: dynamic,
			Authorize: func(callCtx context.Context, payload assettokens.Payload) (bool, error) {
				return appcore.AssetAuthorized(callCtx, store.Snapshot(), *configPath, dynamic, payload)
			}}
		reloads.Add(1)
		go func() {
			defer reloads.Done()
			reloadRuntime(ctx, store, manager, dynamic, db, instanceKey, tokens, *configPath, runtimes, logger)
		}()
	}

	srv, err := api.New(cfg)
	if err != nil {
		return err
	}

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

func reloadRuntime(ctx context.Context, store *config.Store, manager *scheduler.Manager, dynamic *connections.Dynamic, db *storage.Store, instanceKey []byte, tokens *assettokens.Service, configPath string, runtimes *runtimeHolder, logger *slog.Logger) {
	var generation = store.Status().Generation
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		status := store.Status()
		if !status.OK || status.Generation == generation {
			continue
		}
		snapshot := store.Snapshot()
		registry, _, next, err := buildRuntime(ctx, snapshot, status.Generation, configPath, db, instanceKey, tokens, logger)
		if err != nil {
			logger.Error("runtime reload rejected", "error", err)
			continue
		}
		if err = manager.Apply(ctx, next.Definitions); err != nil {
			closeRuntimeGeneration(next, logger)
			logger.Error("runtime reload rejected", "error", err)
			continue
		}
		dynamic.Swap(registry)
		runtimes.Swap(next, logger)
		generation = status.Generation
	}
}

type runtimeHolder struct {
	mu      sync.Mutex
	current *appcore.Generation
}

func (h *runtimeHolder) Swap(next *appcore.Generation, logger *slog.Logger) {
	h.mu.Lock()
	old := h.current
	h.current = next
	h.mu.Unlock()
	closeRuntimeGeneration(old, logger)
}

func (h *runtimeHolder) Close(logger *slog.Logger) {
	h.mu.Lock()
	current := h.current
	h.current = nil
	h.mu.Unlock()
	closeRuntimeGeneration(current, logger)
}

func closeRuntimeGeneration(generation *appcore.Generation, logger *slog.Logger) {
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := generation.Close(closeCtx); err != nil {
		logger.Error("close integration runtime generation", "error", err)
	}
}

func buildRuntime(ctx context.Context, snapshot *config.Snapshot, generation uint64, configPath string, db *storage.Store, instanceKey []byte, tokens *assettokens.Service, logger *slog.Logger) (connections.Registry, map[string]string, *appcore.Generation, error) {
	resolved, diags := secrets.ResolveAll(snapshot.SecretRefs, secrets.DefaultResolver())
	if diags.HasErrors() {
		return nil, nil, nil, fmt.Errorf("resolve secrets:\n%s", diags.String())
	}
	registry, err := connections.New(snapshot.Config.Connections, resolved, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build connection registry: %w", err)
	}
	material, err := connections.MaterialHMACs(snapshot.Config.Connections, resolved, instanceKey)
	if err != nil {
		return nil, nil, nil, err
	}
	revisions, err := db.SyncConnectionRevisions(ctx, material)
	if err != nil {
		return nil, nil, nil, err
	}
	built, err := appcore.BuildGeneration(ctx, snapshot, generation, configPath, registry, revisions, tokens, filepath.Join(db.DataDir(), "wasm-cache"), logger)
	if err != nil {
		return nil, nil, nil, err
	}
	return registry, revisions, built, nil
}

// configLoader is config.Store's Loader for real (non-fixture) configuration: parse, resolve
// secrets, then validateAuthNone - run every time a config is loaded, including a hot reload, not
// only once at startup. Found in review: this check previously ran once, right after the initial
// config.Open, and never again - a config edited live from auth.mode: password to auth.mode: none
// while still referencing secrets would reload successfully and go live with no warning at all. A
// failed check here is an ordinary load diagnostic, so config.Store's own atomic-swap semantics
// apply automatically: the bad snapshot is refused and the previous good one stays live.
func configLoader(override bool) config.Loader {
	return func(path string) (*config.Snapshot, config.Diagnostics) {
		snapshot, diags := config.LoadPath(path)
		if snapshot == nil || diags.HasErrors() {
			return nil, diags
		}
		_, secretDiags := secrets.ResolveAll(snapshot.SecretRefs, secrets.DefaultResolver())
		diags = append(diags, secretDiags...)
		if diags.HasErrors() {
			return nil, diags
		}
		if err := validateAuthNone(snapshot, override); err != nil {
			diags = append(diags, config.Diagnostic{Severity: config.SeverityError, File: path, Message: err.Error()})
			return nil, diags
		}
		return snapshot, diags
	}
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
