// Package api serves the HTTP surface. Milestone A3: the foundation only - health, build
// identity, timeouts, structured logging, panic recovery and graceful shutdown. The dashboard
// endpoints arrive with the configuration and card machinery in Phases C to F.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"veduta.dev/veduta/internal/version"
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
}

// Server wraps the HTTP server and its lifecycle.
type Server struct {
	cfg  Config
	log  *slog.Logger
	http *http.Server
}

// ErrPublicWithoutAuth is returned when a non-loopback bind is attempted before authentication
// exists. It is a refusal to start, not a warning: a warning would be read once and ignored.
var ErrPublicWithoutAuth = errors.New(
	"refusing to bind a non-loopback address without authentication: configure auth, " +
		"bind 127.0.0.1, or pass --i-know-what-im-doing")

// New builds a server. It validates the listen address before any socket is opened.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8099"
	}
	public, err := isPublicAddr(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen address %q: %w", cfg.Listen, err)
	}
	if public && !cfg.AuthConfigured && !cfg.AllowPublicWithoutAuth {
		return nil, ErrPublicWithoutAuth
	}

	s := &Server{cfg: cfg, log: cfg.Logger}
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
			return true, nil
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
	return s.recoverPanics(mux)
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
