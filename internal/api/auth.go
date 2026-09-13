// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	auditlog "veduta.dev/veduta/internal/audit"
	"veduta.dev/veduta/internal/auth"
)

type identityContextKey struct{}

func identityFromContext(ctx context.Context) (auth.Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(auth.Identity)
	return identity, ok
}

func (s *Server) routeAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid login request"})
			return
		}
		session, err := s.cfg.Auth.Login(r.Context(), request.Username, request.Password, r.UserAgent(), auth.ClientIP(r))
		if err != nil {
			status := http.StatusUnauthorized
			switch {
			case errors.Is(err, auth.ErrRateLimited):
				status = http.StatusTooManyRequests
				w.Header().Set("Retry-After", "900")
			case errors.Is(err, auth.ErrBusy):
				// Refused before hashing rather than rejected on credentials - a transient
				// capacity signal, so it must not read as "wrong password".
				status = http.StatusServiceUnavailable
				w.Header().Set("Retry-After", "1")
			}
			s.audit(r.Context(), auditlog.Entry{Actor: request.Username, IP: auth.ClientIP(r), Action: "auth.login", Outcome: "failure"})
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		s.audit(r.Context(), auditlog.Entry{Actor: request.Username, IP: auth.ClientIP(r), Action: "auth.login", Outcome: "success"})
		secure := r.TLS != nil
		// #nosec G124 -- Secure follows the request transport so loopback HTTP remains usable;
		// public deployments are separately gated and H2 handles trusted TLS proxies.
		http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: session.Token, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
		// #nosec G124 -- the CSRF cookie must be readable for double-submit; it remains Strict.
		http.SetCookie(w, &http.Cookie{Name: auth.CSRFCookie, Value: session.CSRFToken, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), Secure: secure, SameSite: http.SameSiteStrictMode})
		writeJSON(w, http.StatusCreated, map[string]any{"username": request.Username, "mode": "password", "capabilities": []string{"admin"}, "csrfToken": session.CSRFToken})
	})
	mux.HandleFunc("DELETE /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		identity, _ := identityFromContext(r.Context())
		cookie, _ := r.Cookie(auth.SessionCookie)
		if cookie != nil {
			if err := s.cfg.Auth.Logout(r.Context(), cookie.Value); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
				return
			}
		}
		s.audit(r.Context(), auditlog.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "auth.logout", Outcome: "success"})
		clearAuthCookies(w, r.TLS != nil)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/auth/sudo", func(w http.ResponseWriter, r *http.Request) {
		identity, _ := identityFromContext(r.Context())
		var request struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sudo request"})
			return
		}
		sessionCookie, _ := r.Cookie(auth.SessionCookie)
		if sessionCookie == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": auth.ErrNoSession.Error()})
			return
		}
		if err := s.cfg.Auth.OpenSudo(r.Context(), sessionCookie.Value, request.Password, auth.ClientIP(r)); err != nil {
			s.audit(r.Context(), auditlog.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "auth.sudo", Outcome: "failure"})
			status, message := http.StatusUnauthorized, auth.ErrInvalidCredentials.Error()
			switch {
			case errors.Is(err, auth.ErrRateLimited):
				status, message = http.StatusTooManyRequests, err.Error()
				w.Header().Set("Retry-After", "900")
			case errors.Is(err, auth.ErrBusy):
				status, message = http.StatusServiceUnavailable, err.Error()
				w.Header().Set("Retry-After", "1")
			}
			writeJSON(w, status, map[string]string{"error": message})
			return
		}
		s.audit(r.Context(), auditlog.Entry{Actor: identity.Username, IP: auth.ClientIP(r), Action: "auth.sudo", Outcome: "success"})
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Server) routeIdentity(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := identityFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"username": "anonymous", "mode": "none", "capabilities": []string{}})
			return
		}
		csrfToken := ""
		if s.cfg.Auth != nil {
			sessionCookie, _ := r.Cookie(auth.SessionCookie)
			var err error
			_, csrfToken, err = s.cfg.Auth.Authenticate(r.Context(), sessionCookie.Value)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": auth.ErrNoSession.Error()})
				return
			}
		}
		capabilities := []string{"viewer"}
		if identity.Admin {
			capabilities = append(capabilities, "admin")
		}
		writeJSON(w, http.StatusOK, map[string]any{"username": identity.Username, "mode": identity.Mode, "capabilities": capabilities, "csrfToken": csrfToken})
	})
}

func (s *Server) audit(ctx context.Context, entry auditlog.Entry) {
	if s.cfg.Audit == nil {
		return
	}
	if err := s.cfg.Audit.Record(ctx, entry); err != nil {
		s.log.Error("write audit record", "action", entry.Action, "error", err)
	}
}

func (s *Server) allowsPrivileged(identity auth.Identity) bool {
	if s.cfg.Auth != nil {
		return s.cfg.Auth.AllowsPrivileged(identity)
	}
	if s.cfg.ForwardAuth != nil {
		return s.cfg.ForwardAuth.AllowsPrivileged(identity)
	}
	return false
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	if s.cfg.Auth == nil && s.cfg.ForwardAuth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /notices joins /health and /version as unauthenticated: it is a licence disclosure that
		// must travel with the distribution, and one only an administrator can reach is not one.
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/version" || r.URL.Path == "/api/v1/notices" || (r.URL.Path == "/api/v1/auth/session" && r.Method == http.MethodPost && s.cfg.Auth != nil) || !isAPIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		var identity auth.Identity
		var err error
		if s.cfg.ForwardAuth != nil {
			identity, err = s.cfg.ForwardAuth.Authenticate(r)
			if err == nil && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				err = s.cfg.ForwardAuth.CheckMutation(r)
			}
		} else {
			sessionCookie, cookieErr := r.Cookie(auth.SessionCookie)
			switch {
			case cookieErr != nil:
				err = auth.ErrNoSession
			case r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions:
				identity, _, err = s.cfg.Auth.Authenticate(r.Context(), sessionCookie.Value)
			default:
				csrfCookie, csrfErr := r.Cookie(auth.CSRFCookie)
				if csrfErr != nil {
					err = auth.ErrCSRF
				} else {
					identity, err = s.cfg.Auth.CheckCSRF(r.Context(), sessionCookie.Value, csrfCookie.Value, r.Header.Get(auth.CSRFHeader))
				}
			}
		}
		if err != nil {
			if s.cfg.ForwardAuth != nil {
				w.Header().Set("X-Veduta-Auth-Mode", "forward")
			}
			status := http.StatusUnauthorized
			if errors.Is(err, auth.ErrCSRF) {
				status = http.StatusForbidden
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityContextKey{}, identity)))
	})
}

func clearAuthCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{auth.SessionCookie, auth.CSRFCookie} {
		// #nosec G124 -- deletion must use the same transport and HttpOnly attributes as creation.
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: name == auth.SessionCookie, Secure: secure, SameSite: http.SameSiteStrictMode})
	}
}

func isAPIPath(path string) bool {
	return path == "/api" || path == "/api/" || len(path) > len("/api/") && path[:len("/api/")] == "/api/"
}
