// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
			if errors.Is(err, auth.ErrRateLimited) {
				status = http.StatusTooManyRequests
				w.Header().Set("Retry-After", "900")
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		secure := r.TLS != nil
		// #nosec G124 -- Secure follows the request transport so loopback HTTP remains usable;
		// public deployments are separately gated and H2 handles trusted TLS proxies.
		http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: session.Token, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
		// #nosec G124 -- the CSRF cookie must be readable for double-submit; it remains Strict.
		http.SetCookie(w, &http.Cookie{Name: auth.CSRFCookie, Value: session.CSRFToken, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), Secure: secure, SameSite: http.SameSiteStrictMode})
		writeJSON(w, http.StatusCreated, map[string]any{"username": request.Username, "mode": "password", "capabilities": []string{"admin"}, "csrfToken": session.CSRFToken})
	})
	mux.HandleFunc("DELETE /api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie(auth.SessionCookie)
		if cookie != nil {
			if err := s.cfg.Auth.Logout(r.Context(), cookie.Value); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
				return
			}
		}
		clearAuthCookies(w, r.TLS != nil)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, _ := r.Cookie(auth.SessionCookie)
		identity, csrfToken, err := s.cfg.Auth.Authenticate(r.Context(), sessionCookie.Value)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": auth.ErrNoSession.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"username": identity.Username, "mode": "password", "capabilities": []string{"admin"}, "csrfToken": csrfToken})
	})
}

func clearAuthCookies(w http.ResponseWriter, secure bool) {
	for _, name := range []string{auth.SessionCookie, auth.CSRFCookie} {
		// #nosec G124 -- deletion must use the same transport and HttpOnly attributes as creation.
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: name == auth.SessionCookie, Secure: secure, SameSite: http.SameSiteStrictMode})
	}
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	if s.cfg.Auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" || r.URL.Path == "/api/v1/version" || (r.URL.Path == "/api/v1/auth/session" && r.Method == http.MethodPost) || !isAPIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		sessionCookie, err := r.Cookie(auth.SessionCookie)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": auth.ErrNoSession.Error()})
			return
		}
		var identity auth.Identity
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			identity, _, err = s.cfg.Auth.Authenticate(r.Context(), sessionCookie.Value)
		} else {
			csrfCookie, cookieErr := r.Cookie(auth.CSRFCookie)
			if cookieErr != nil {
				err = auth.ErrCSRF
			} else {
				identity, err = s.cfg.Auth.CheckCSRF(r.Context(), sessionCookie.Value, csrfCookie.Value, r.Header.Get(auth.CSRFHeader))
			}
		}
		if err != nil {
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

func isAPIPath(path string) bool {
	return path == "/api" || path == "/api/" || len(path) > len("/api/") && path[:len("/api/")] == "/api/"
}
