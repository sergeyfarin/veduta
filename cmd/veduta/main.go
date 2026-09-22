// SPDX-License-Identifier: AGPL-3.0-or-later

// Command veduta serves the dashboard.
//
//	veduta serve [--listen host:port]   run the HTTP server
//	veduta health [--addr url]          probe a running instance; non-zero exit if unhealthy
//	veduta version [--json]             print build identity
//	veduta init [--config path]         write a first configuration with a generated password
//	veduta auth hash                    read a password from stdin, print its Argon2id PHC hash
//	veduta manifest digest <file>...    print the canonical digest of an integration manifest
//	veduta integration list             show every declared integration's lock status
//	veduta integration diff <id>        print the permission diff since the last approval
//	veduta integration approve <id>     review the diff and record approval in veduta.lock.yaml
//	veduta plugin validate <file.wasm>   run sandbox and ABI conformance checks
//	veduta import homepage --dir <path>  import Homepage configuration
//	veduta --check-config [--config path]   validate a config file and print diagnostics
//
// Password authentication permits a non-loopback bind; other modes remain gated until H2.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"veduta.dev/veduta/internal/api"
	appcore "veduta.dev/veduta/internal/app"
	"veduta.dev/veduta/internal/audit"
	"veduta.dev/veduta/internal/auth"
	"veduta.dev/veduta/internal/canonical"
	"veduta.dev/veduta/internal/capabilities"
	assettokens "veduta.dev/veduta/internal/capabilities/assets"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/fixtures"
	"veduta.dev/veduta/internal/icons"
	"veduta.dev/veduta/internal/notify"
	"veduta.dev/veduta/internal/rules"
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
	case "health":
		return healthCmd(args)
	case "init":
		return initCmd(args)
	case "auth":
		return authCmd(args)
	case "manifest":
		return manifestCmd(args)
	case "integration":
		return integrationCmd(args)
	case "plugin":
		return pluginCmd(args)
	case "import":
		return importCmd(args)
	default:
		return fmt.Errorf("unknown command %q (try: serve, init, health, version, auth, manifest, integration, plugin, import)", cmd)
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
		"allow a non-loopback bind without active authentication")
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
		cfg.ConfigDir = filepath.Dir(*configPath)
		cfg.Listen = store.Snapshot().Config.Server.Listen
		if *listen != "" {
			cfg.Listen = *listen
		}

		snapshot := store.Snapshot()
		cfg.AuthMode = snapshot.Config.Auth.Mode
		dir := snapshot.Config.Server.DataDir
		if *dataDir != "" {
			dir = *dataDir
		}
		db, err := storage.Open(ctx, dir)
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}
		defer func() { _ = db.Close() }()
		resolvedSecrets, secretDiags := secrets.ResolveAll(snapshot.SecretRefs, secrets.DefaultResolver())
		if secretDiags.HasErrors() {
			return fmt.Errorf("resolve authentication secrets:\n%s", secretDiags.String())
		}
		auditLog, auditErr := audit.New(db)
		if auditErr != nil {
			return fmt.Errorf("configure audit log: %w", auditErr)
		}
		cfg.Audit = auditLog
		cfg.EventStore = db
		switch snapshot.Config.Auth.Mode {
		case config.AuthPassword:
			admin := snapshot.Config.Auth.Admin
			authService, authErr := auth.New(ctx, auth.Config{Store: db, Username: admin.Username, PasswordHash: admin.PasswordHash, ResolvedSecrets: resolvedSecrets, SessionTTL: snapshot.Config.Auth.SessionTTL})
			if authErr != nil {
				return fmt.Errorf("configure authentication: %w", authErr)
			}
			cfg.Auth = authService
			cfg.AuthConfigured = true
		case config.AuthForward:
			forward := snapshot.Config.Auth.Forward
			forwardAuth, forwardErr := auth.NewForward(auth.ForwardConfig{TrustedProxies: forward.TrustedProxies, UserHeader: forward.UserHeader, GroupsHeader: forward.GroupsHeader, AdminGroups: forward.AdminGroups, PrivilegedOperations: string(forward.PrivilegedOperations)})
			if forwardErr != nil {
				return fmt.Errorf("configure forward authentication: %w", forwardErr)
			}
			cfg.ForwardAuth = forwardAuth
			cfg.AuthConfigured = true
		}
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
		registry, _, runtimeGeneration, notificationChannels, err := buildRuntime(ctx, snapshot, store.Status().Generation, *configPath, db, instanceKey, tokens, auditLog, logger)
		if err != nil {
			return fmt.Errorf("build runtime: %w", err)
		}
		if err = manager.Apply(ctx, runtimeGeneration.Definitions); err != nil {
			closeRuntimeGeneration(runtimeGeneration, logger)
			return fmt.Errorf("start schedules: %w", err)
		}
		dispatcher := notify.NewDispatcher(db, notificationChannels, logger, notify.DispatcherConfig{})
		ruleManager := rules.NewWithNotifier(ctx, db, manager, logger, dispatcher)
		if err = ruleManager.Apply(ctx, runtimeGeneration.RuleDefinitions, runtimeGeneration.RuleDeclarations); err != nil {
			ruleManager.Close()
			closeRuntimeGeneration(runtimeGeneration, logger)
			return fmt.Errorf("start rules: %w", err)
		}
		if err = ruleManager.Evaluate(ctx); err != nil {
			ruleManager.Close()
			closeRuntimeGeneration(runtimeGeneration, logger)
			return fmt.Errorf("evaluate rules: %w", err)
		}
		recordGenerationAudit(ctx, auditLog, store.Status().Generation, runtimeGeneration, logger)
		runtimes := &runtimeHolder{current: runtimeGeneration}
		var reloads sync.WaitGroup
		defer func() {
			stop()
			ruleManager.Close()
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
		cfg.IconProxy = icons.New(assetCache)
		cfg.AssetProxy = &api.AssetProxy{Tokens: tokens, Store: db, Cache: assetCache, Registry: dynamic,
			Authorize: func(callCtx context.Context, payload assettokens.Payload) (bool, error) {
				return appcore.AssetAuthorized(callCtx, store.Snapshot(), *configPath, dynamic, payload)
			}}
		reloads.Add(1)
		go func() {
			defer reloads.Done()
			if dispatchErr := dispatcher.Run(ctx); dispatchErr != nil && !errors.Is(dispatchErr, context.Canceled) {
				logger.Error("notification dispatcher stopped", "error", dispatchErr)
			}
		}()
		store.SetActivator(func(activateCtx context.Context, candidate *config.Snapshot, generation uint64) error {
			return activateRuntime(activateCtx, candidate, generation, store, manager, ruleManager, dispatcher, dynamic, db, instanceKey, tokens, cfg.Auth, cfg.ForwardAuth, auditLog, *configPath, runtimes, logger)
		})
	}

	srv, err := api.New(cfg)
	if err != nil {
		return err
	}

	// Registered after the server exists and, more importantly, fired after the store has
	// published the new snapshot - a layout refetch triggered any earlier would read the
	// generation that is being replaced.
	if cfg.ConfigStore != nil {
		cfg.ConfigStore.SetPublished(srv.NotifyConfigChanged)
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

func activateRuntime(ctx context.Context, snapshot *config.Snapshot, generation uint64, store *config.Store, manager *scheduler.Manager, ruleManager *rules.Manager, dispatcher *notify.Dispatcher, dynamic *connections.Dynamic, db *storage.Store, instanceKey []byte, tokens *assettokens.Service, authService *auth.Service, forwardAuth *auth.Forward, auditLog *audit.Log, configPath string, runtimes *runtimeHolder, logger *slog.Logger) error {
	registry, resolvedSecrets, next, nextChannels, err := buildRuntime(ctx, snapshot, generation, configPath, db, instanceKey, tokens, auditLog, logger)
	if err != nil {
		return err
	}
	keepNext := false
	defer func() {
		if !keepNext {
			closeRuntimeGeneration(next, logger)
		}
	}()

	previousSnapshot := store.Snapshot()
	previousScheduler := manager.Definitions()
	previousRules, previousDeclarations := ruleManager.Definitions()
	previousChannels := dispatcher.Channels()
	authChanged := false
	rollback := func(cause error, schedulerChanged, rulesChanged bool) error {
		var rollbackErrors []error
		if schedulerChanged {
			rollbackErrors = appendIfError(rollbackErrors, manager.Apply(ctx, previousScheduler))
		}
		if rulesChanged {
			if rollbackErr := ruleManager.Apply(ctx, previousRules, previousDeclarations); rollbackErr == nil {
				rollbackErr = ruleManager.Evaluate(ctx)
				rollbackErrors = appendIfError(rollbackErrors, rollbackErr)
			} else {
				rollbackErrors = append(rollbackErrors, rollbackErr)
			}
			dispatcher.Apply(previousChannels)
		}
		if authChanged {
			rollbackErrors = appendIfError(rollbackErrors, reconfigureAuthentication(ctx, previousSnapshot, authService, forwardAuth))
		}
		if len(rollbackErrors) > 0 {
			return errors.Join(append([]error{cause, errors.New("runtime rollback failed")}, rollbackErrors...)...)
		}
		return cause
	}

	if err = reconfigureAuthenticationResolved(ctx, snapshot, resolvedSecrets, authService, forwardAuth); err != nil {
		return err
	}
	authChanged = authService != nil || forwardAuth != nil
	if err = ruleManager.Apply(ctx, next.RuleDefinitions, next.RuleDeclarations); err != nil {
		return rollback(err, false, false)
	}
	dispatcher.Apply(nextChannels)
	if err = manager.Apply(ctx, next.Definitions); err != nil {
		return rollback(err, false, true)
	}
	if err = ruleManager.Evaluate(ctx); err != nil {
		return rollback(err, true, true)
	}
	dynamic.Swap(registry)
	runtimes.Swap(next, logger)
	keepNext = true
	recordGenerationAudit(ctx, auditLog, generation, next, logger)
	return nil
}

func appendIfError(values []error, err error) []error {
	if err != nil {
		return append(values, err)
	}
	return values
}

func reconfigureAuthentication(ctx context.Context, snapshot *config.Snapshot, authService *auth.Service, forwardAuth *auth.Forward) error {
	resolved, diags := secrets.ResolveAll(snapshot.SecretRefs, secrets.DefaultResolver())
	if diags.HasErrors() {
		return errors.New(diags.String())
	}
	return reconfigureAuthenticationResolved(ctx, snapshot, resolved, authService, forwardAuth)
}

func reconfigureAuthenticationResolved(ctx context.Context, snapshot *config.Snapshot, resolved map[string]secrets.Value, authService *auth.Service, forwardAuth *auth.Forward) error {
	if authService != nil && snapshot.Config.Auth.Mode == config.AuthPassword {
		admin := snapshot.Config.Auth.Admin
		if err := authService.Reconfigure(ctx, auth.Config{Username: admin.Username, PasswordHash: admin.PasswordHash, ResolvedSecrets: resolved, SessionTTL: snapshot.Config.Auth.SessionTTL}); err != nil {
			return err
		}
	}
	if forwardAuth != nil && snapshot.Config.Auth.Mode == config.AuthForward {
		forward := snapshot.Config.Auth.Forward
		return forwardAuth.Reconfigure(auth.ForwardConfig{TrustedProxies: forward.TrustedProxies, UserHeader: forward.UserHeader, GroupsHeader: forward.GroupsHeader, AdminGroups: forward.AdminGroups, PrivilegedOperations: string(forward.PrivilegedOperations)})
	}
	return nil
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

func buildRuntime(ctx context.Context, snapshot *config.Snapshot, generation uint64, configPath string, db *storage.Store, instanceKey []byte, tokens *assettokens.Service, auditLog *audit.Log, logger *slog.Logger) (connections.Registry, map[string]secrets.Value, *appcore.Generation, map[string]notify.Channel, error) {
	resolved, diags := secrets.ResolveAll(snapshot.SecretRefs, secrets.DefaultResolver())
	if diags.HasErrors() {
		return nil, nil, nil, nil, fmt.Errorf("resolve secrets:\n%s", diags.String())
	}
	registry, err := connections.New(snapshot.Config.Connections, resolved, logger)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("build connection registry: %w", err)
	}
	material, err := connections.MaterialHMACs(snapshot.Config.Connections, resolved, instanceKey)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	revisions, err := db.SyncConnectionRevisions(ctx, material)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	eventSink := func(eventCtx context.Context, pluginID string, event capabilities.Event) error {
		data, marshalErr := json.Marshal(event.Data)
		if marshalErr != nil {
			return marshalErr
		}
		return db.AppendEvent(eventCtx, storage.Event{Type: event.Type, Severity: "info", Source: pluginID, Data: data})
	}
	built, err := appcore.BuildGenerationWithAuditAndEvents(ctx, snapshot, generation, configPath, registry, revisions, tokens, filepath.Join(db.DataDir(), "wasm-cache"), auditLog, eventSink, logger)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	channels, err := notify.BuildChannels(snapshot.Config.Notifications.Channels, resolved, nil)
	if err != nil {
		_ = built.Close(context.Background())
		return nil, nil, nil, nil, err
	}
	return registry, resolved, built, channels, nil
}

func recordGenerationAudit(ctx context.Context, log *audit.Log, generation uint64, built *appcore.Generation, logger *slog.Logger) {
	if err := log.Record(ctx, audit.Entry{Actor: "system", Action: "config.apply", Outcome: "success", Detail: map[string]uint64{"generation": generation}}); err != nil {
		logger.Error("write config audit record", "error", err)
	}
	for _, plugin := range built.PluginLoads {
		if err := log.Record(ctx, audit.Entry{Actor: "system", Action: "plugin.load", Target: plugin.ID, Outcome: "success", Detail: plugin}); err != nil {
			logger.Error("write plugin audit record", "plugin", plugin.ID, "error", err)
		}
	}
}

// configLoader is config.Store's Loader for real (non-fixture) configuration: parse, resolve
// secrets, then validateAuthNone - run every time a config is loaded, including a hot reload, not
// only once at startup. Found in review: this check previously ran once, right after the initial
// config.Open, and never again - a config edited live from auth.mode: password to auth.mode: none
// while still referencing secrets would reload successfully and go live with no warning at all. A
// failed check here is an ordinary load diagnostic, so config.Store's own atomic-swap semantics
// apply automatically: the bad snapshot is refused and the previous good one stays live.
func configLoader(override bool) config.Loader {
	var mu sync.Mutex
	var initialMode config.AuthMode
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
		mu.Lock()
		defer mu.Unlock()
		if initialMode == "" {
			initialMode = snapshot.Config.Auth.Mode
		} else if snapshot.Config.Auth.Mode != initialMode {
			diags = append(diags, config.Diagnostic{Severity: config.SeverityError, File: path, Message: "auth.mode changes require a server restart"})
			return nil, diags
		}
		return snapshot, diags
	}
}

func validateAuthNone(snapshot *config.Snapshot, override bool) error {
	if snapshot.Config.Auth.Mode == config.AuthNone && !override && (len(snapshot.SecretRefs) > 0 || configuredActions(snapshot)) {
		return errors.New("auth.mode is none but the configuration enables actions or references secrets; pass --i-know-what-im-doing to acknowledge the risk")
	}
	return nil
}

func configuredActions(snapshot *config.Snapshot) bool {
	for _, connection := range snapshot.Config.Connections {
		if connection.Docker != nil && connection.Docker.AllowActions {
			return true
		}
	}
	for _, section := range snapshot.Config.Sections {
		for _, card := range section.Cards {
			if containsAction(card.View) {
				return true
			}
		}
	}
	return false
}

func containsAction(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if blockType, _ := typed["type"].(string); blockType == "actions" {
			return true
		}
		for _, child := range typed {
			if containsAction(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsAction(child) {
				return true
			}
		}
	}
	return false
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

// healthCmd probes a running instance's /api/v1/health and exits non-zero if it is not serving.
// It exists because the container image is distroless: the binary is the only executable in it,
// so a Docker HEALTHCHECK (or a compose healthcheck) has nothing else to call. Keeping the probe
// in the binary also means it cannot drift from the endpoint it checks.
func healthCmd(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	addr := fs.String("addr", "http://127.0.0.1:8099", "base URL of the instance to probe")
	timeout := fs.Duration("timeout", 5*time.Second, "how long to wait for a response")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *addr+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection can be reused, and bound it: this is an unauthenticated endpoint
	// and the probe should not be a way to make the CLI read an unbounded body.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned HTTP %d", resp.StatusCode)
	}
	return nil
}
