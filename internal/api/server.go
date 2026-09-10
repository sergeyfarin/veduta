// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api serves the HTTP surface. Milestone A3 supplied the foundation - health, build
// identity, timeouts, structured logging, panic recovery and graceful shutdown. The same route
// table serves either checked-in fixtures or the production config/scheduler/asset pipeline.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"veduta.dev/veduta/internal/audit"
	"veduta.dev/veduta/internal/auth"
	"veduta.dev/veduta/internal/config"
	"veduta.dev/veduta/internal/connections"
	"veduta.dev/veduta/internal/fixtures"
	"veduta.dev/veduta/internal/icons"
	"veduta.dev/veduta/internal/notices"
	"veduta.dev/veduta/internal/scheduler"
	"veduta.dev/veduta/internal/state"
	"veduta.dev/veduta/internal/storage"
	"veduta.dev/veduta/internal/version"
	"veduta.dev/veduta/web"
)

// Config is everything the server needs to start.
type Config struct {
	Listen string
	Logger *slog.Logger
	// AuthConfigured reports whether authentication is in force. Until it is, the server
	// refuses to bind anything but loopback: the dashboard holds service credentials from
	// Phase D onward, and its default listener used to be all-interfaces.
	// See docs/01-architecture.md decision D46.
	AuthConfigured bool
	// AllowPublicWithoutAuth is the operator's explicit override for that refusal.
	AllowPublicWithoutAuth bool
	// Auth enforces password sessions when non-nil.
	Auth *auth.Service
	// ForwardAuth accepts identity headers only from configured proxy networks.
	ForwardAuth *auth.Forward
	// Audit persists security-relevant mutations and authentication events.
	Audit *audit.Log
	// AuthMode records the explicit production policy. The zero value preserves lightweight
	// handler construction in package tests; serve always sets it from the validated config.
	AuthMode config.AuthMode

	// Assets overrides the embedded frontend. Left nil it uses the build compiled into the
	// binary; tests set it so their results do not depend on whether anyone ran `pnpm build`.
	Assets        fs.FS
	AssetsPresent bool

	// Fixtures, when non-nil, serves the checked-in showcase dashboard from GET /dashboard,
	// /cards and /assets/{token} - a dev-only stand-in for configuration and the scheduler, and
	// what the visual regression suite (milestone B5) runs against. Never set in production.
	// Mutually exclusive with ConfigStore - see New, which refuses both being set rather than
	// letting http.ServeMux panic on the resulting duplicate route registration.
	Fixtures *fixtures.Bundle

	// ConfigDir is the directory holding veduta.yaml. It is what a relative
	// dashboard.background resolves against, so a background does not depend on the working
	// directory veduta was started from.
	ConfigDir string

	// ConfigStore is the atomically reloadable real configuration. Until the scheduler lands,
	// configured cards are returned honestly as pending so the production render path is usable.
	// Mutually exclusive with Fixtures.
	ConfigStore *config.Store

	// Registry is the credential boundary. Production passes a connections.Dynamic whose whole
	// resolved registry is replaced on accepted config generations. nil disables connection APIs.
	Registry   connections.Registry
	Scheduler  *scheduler.Manager
	AssetProxy *AssetProxy
	IconProxy  *icons.Resolver
	EventStore *storage.Store
}

// errBothFixturesAndConfigStore documents why New refuses to build a server with both set: they
// register the identical GET /dashboard and /cards patterns, which panics inside http.ServeMux
// rather than failing cleanly - confirmed directly by constructing exactly this Config before
// deciding a guard was needed, not assumed from reading the route tables.
var errBothFixturesAndConfigStore = errors.New(
	"api: Config.Fixtures and Config.ConfigStore are mutually exclusive (both register " +
		"GET /dashboard and /cards); set at most one")

// Server wraps the HTTP server and its lifecycle.
type Server struct {
	cfg  Config
	log  *slog.Logger
	http *http.Server

	// approveMu serialises POST /api/v1/integrations/{id}/approve's read-modify-write of
	// veduta.lock.yaml - found missing in review: two concurrent approvals (even of different
	// integrations) could each read the same lock, add their own entry to their own in-memory
	// copy, and the second WriteLock silently discards the first's addition. This closes the
	// in-process race; a concurrent writer in a different process (the CLI, running at the same
	// moment) is a separate, recorded gap - see docs/03-backlog.md.
	approveMu sync.Mutex
	hub       *sseHub
}

// ErrPublicWithoutAuth is returned when a non-loopback bind is attempted before authentication
// exists. It is a refusal to start, not a warning: a warning would be read once and ignored.
var ErrPublicWithoutAuth = errors.New(
	"refusing to bind a non-loopback address without authentication: configure auth, " +
		"bind 127.0.0.1, or pass --i-know-what-im-doing")

// New builds a server. It validates the listen address before any socket is opened.
func New(cfg Config) (*Server, error) {
	if cfg.Fixtures != nil && cfg.ConfigStore != nil {
		return nil, errBothFixturesAndConfigStore
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8099"
	}
	if cfg.Auth != nil || cfg.ForwardAuth != nil {
		cfg.AuthConfigured = true
	}
	public, err := isPublicAddr(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen address %q: %w", cfg.Listen, err)
	}
	if public && !cfg.AuthConfigured && !cfg.AllowPublicWithoutAuth {
		return nil, ErrPublicWithoutAuth
	}

	s := &Server{cfg: cfg, log: cfg.Logger}
	if cfg.Scheduler != nil {
		s.hub = newSSEHub(cfg.Scheduler)
	}
	s.http = &http.Server{
		Addr:              cfg.Listen,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	return s, nil
}

// isPublicAddr reports whether a listen address would accept connections from outside the host.
// An unspecified host ("" or ":8099") binds every interface, which is the case that caught the
// original plan out.
func isPublicAddr(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, err
	}
	if host == "" {
		return true, nil // ":8099" means all interfaces
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname: resolve conservatively and treat anything non-loopback as public.
		ips, err := net.LookupIP(host)
		if err != nil {
			// Fail closed: a name that will not resolve now might resolve to a public address
			// later, and this gate exists precisely to be conservative.
			return true, nil //nolint:nilerr // deliberate: unresolvable means "treat as public"
		}
		for _, candidate := range ips {
			if !candidate.IsLoopback() {
				return true, nil
			}
		}
		return false, nil
	}
	if ip.IsUnspecified() {
		return true, nil
	}
	return !ip.IsLoopback(), nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/notices", s.handleNotices)
	s.routeIdentity(mux)
	if s.cfg.Auth != nil {
		s.routeAuth(mux)
	}
	if s.cfg.EventStore != nil {
		s.routeEvents(mux)
	}
	if s.cfg.IconProxy != nil {
		s.routeIcons(mux)
	}
	if s.cfg.ConfigStore != nil {
		s.routeConfig(mux)
		s.routeBackground(mux)
		if s.hub != nil {
			s.routeSSE(mux)
		}
		s.routeIntegrations(mux)
		if s.cfg.Registry != nil {
			s.routeConnections(mux)
		}
		if s.cfg.AssetProxy != nil {
			s.routeAssets(mux)
		}
	}

	if s.cfg.Fixtures != nil {
		s.routeFixtures(mux)
	}

	assets, present := s.cfg.Assets, s.cfg.AssetsPresent
	if assets == nil {
		assets, present = web.Assets()
	}
	if !present {
		s.log.Warn("no frontend build embedded; serving the API only",
			"hint", "run `pnpm build` and rebuild the binary")
	}
	mux.Handle("/", s.staticHandler(assets, present))

	return s.securityHeaders(s.recoverPanics(s.limitBody(s.authenticate(mux))))
}

// securityHeaders applies browser hardening to every response, including authentication errors,
// API 404s and panic recovery. Handlers may replace Cache-Control and CSP with a narrower policy
// for immutable images or the SPA document.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if isAPIPath(r.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routeConfig(mux *http.ServeMux) {
	store := s.cfg.ConfigStore
	mux.HandleFunc("GET /api/v1/config/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.Status())
	})
	mux.HandleFunc("GET /api/v1/dashboard", func(w http.ResponseWriter, r *http.Request) {
		snapshot := store.Snapshot()
		if snapshot == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no valid configuration loaded"})
			return
		}
		type card struct {
			ID    string      `json:"id"`
			Title string      `json:"title"`
			Icon  string      `json:"icon,omitempty"`
			Href  string      `json:"href,omitempty"`
			Span  config.Span `json:"span"`
		}
		type section struct {
			Title string `json:"title,omitempty"`
			Cards []card `json:"cards"`
		}
		sections := make([]section, 0, len(snapshot.Config.Sections))
		for _, configured := range snapshot.Config.Sections {
			out := section{Title: configured.Title, Cards: make([]card, 0, len(configured.Cards))}
			for _, c := range configured.Cards {
				title := c.Title
				if title == "" {
					title = c.ID
				}
				out.Cards = append(out.Cards, card{ID: c.ID, Title: title, Icon: c.Icon, Href: c.Href, Span: c.Span})
			}
			sections = append(sections, out)
		}
		appearance := snapshot.Config.Dashboard.Appearance
		if appearance == "" {
			appearance = "clean"
		}
		// A boolean, not the path: the filesystem layout of the host is not the browser's
		// business, and the image is always served from the one fixed route.
		body := map[string]any{"appearance": appearance, "sections": sections}
		if snapshot.Config.Dashboard.Background != "" {
			body["background"] = true
		}
		writeJSON(w, http.StatusOK, body)
	})
	mux.HandleFunc("GET /api/v1/cards", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Scheduler != nil {
			writeJSON(w, http.StatusOK, s.cfg.Scheduler.States())
			return
		}
		snapshot := store.Snapshot()
		cards := make([]state.CardState, 0)
		if snapshot != nil {
			for _, section := range snapshot.Config.Sections {
				for _, c := range section.Cards {
					cards = append(cards, state.Pending(c.ID))
				}
			}
		}
		writeJSON(w, http.StatusOK, cards)
	})
	if s.cfg.Scheduler != nil {
		mux.HandleFunc("GET /api/v1/cards/{id}", func(w http.ResponseWriter, r *http.Request) {
			cs, ok := s.cfg.Scheduler.State(r.PathValue("id"))
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such card"})
				return
			}
			writeJSON(w, http.StatusOK, cs)
		})
		mux.HandleFunc("POST /api/v1/cards/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			identity, _ := identityFromContext(r.Context())
			if err := s.cfg.Scheduler.RefreshNow(r.Context(), id); err != nil {
				s.audit(r.Context(), audit.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "card.refresh", Target: id, Outcome: "failure"})
				status := http.StatusInternalServerError
				if errors.Is(err, scheduler.ErrUnknownCard) {
					status = http.StatusNotFound
				} else if errors.Is(err, scheduler.ErrRateLimited) {
					status = http.StatusTooManyRequests
				}
				writeJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			s.audit(r.Context(), audit.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "card.refresh", Target: id, Outcome: "success"})
			cs, _ := s.cfg.Scheduler.State(id)
			writeJSON(w, http.StatusOK, cs)
		})
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleVersion also carries the AGPL section 13 source URL for the exact commit this binary
// was built from: anyone interacting with a running instance over a network must be offered
// the corresponding source.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, version.Current())
}

// handleNotices serves the third-party attribution document. Like /version it is unauthenticated:
// the licences of the bundled code require the notice to travel with the distribution, and a
// disclosure that only logged-in administrators can reach is not one. It is served as plain text
// rather than markdown so browsers display it instead of offering a download.
func (s *Server) handleNotices(w http.ResponseWriter, _ *http.Request) {
	// The document is fixed at build time, so it is safe to cache for a long while; a new build
	// serves a new one.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.WriteString(w, notices.Markdown())
}

// maxRequestBodyBytes bounds every request body server-wide - milestone A3's own "Creates" list
// promised "max header/body bytes" alongside the header limit already enforced by
// http.Server.MaxHeaderBytes, but nothing ever added the body half; found in review, still
// self-documented in docs/02-implementation-plan.md as an A3 gap ("Request-id middleware and
// config flags remain" did not mention this one). 1 MiB is generous for every JSON body this API
// accepts today (an approval's grants list is the largest, and still far under this).
const maxRequestBodyBytes = 1 << 20

// limitBody wraps every request body in http.MaxBytesReader, so a handler's own json.Decoder
// naturally errors once more than maxRequestBodyBytes has been read, rather than an unbounded
// read exhausting memory before a handler ever gets a chance to reject an oversized request.
func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic serving request", "method", r.Method, "path", r.URL.Path, "panic", v)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Run serves until ctx is cancelled, then drains in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	s.log.Info("listening", "addr", ln.Addr().String(), "version", version.Current().Version)

	errc := make(chan error, 1)
	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		s.log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errc
	}
}

// Handler exposes the routes for testing without binding a socket.
func (s *Server) Handler() http.Handler { return s.routes() }
