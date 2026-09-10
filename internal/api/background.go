// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// maxBackgroundBytes bounds what the server will read and hand to every viewer on every cold
// load. A wallpaper is legitimately larger than a card thumbnail, but it is not unbounded: this
// file is served to each browser that opens the dashboard.
const maxBackgroundBytes = 8 << 20

// resolveBackground turns dashboard.background into an absolute path. Relative paths resolve
// against the configuration file's directory rather than the process working directory - the same
// rule integrations.ResolveSource uses, so a background behaves identically whichever directory
// veduta was started from.
func resolveBackground(configDir, raw string) string {
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Clean(filepath.Join(configDir, raw))
}

// routeBackground serves the configured background image.
//
// It sniffs content rather than trusting the extension, and refuses anything that is not a
// supported image. That matters less as a privilege boundary - the operator wrote the config and
// could already point dataDir anywhere - than as a blast radius limit on a typo: without the
// check, a mistyped path would turn this route into an arbitrary file read served to every viewer
// of the dashboard. Sniffing means a wrong path returns 404 instead of leaking a file.
func (s *Server) routeBackground(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/background", func(w http.ResponseWriter, r *http.Request) {
		path := s.backgroundPath()
		if path == "" {
			http.NotFound(w, r)
			return
		}
		body, modTime, err := readBackground(path)
		if err != nil {
			// Deliberately not 500: whether the path is missing, unreadable or not an image is
			// not something a viewer can act on, and distinguishing them here would report on
			// the server's filesystem to anyone who can reach the route.
			s.log.Error("background image unavailable", "path", path, "error", err)
			http.NotFound(w, r)
			return
		}
		contentType, ok := sniffImage(body)
		if !ok {
			s.log.Error("background image rejected: not a supported image", "path", path)
			http.NotFound(w, r)
			return
		}
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		http.ServeContent(w, r, filepath.Base(path), modTime, bytes.NewReader(body))
	})
}

// backgroundPath reads the current generation's background, so a config reload that changes or
// removes the image takes effect without a restart.
func (s *Server) backgroundPath() string {
	if s.cfg.ConfigStore == nil {
		return ""
	}
	snapshot := s.cfg.ConfigStore.Snapshot()
	if snapshot == nil || snapshot.Config.Dashboard.Background == "" {
		return ""
	}
	return resolveBackground(s.cfg.ConfigDir, snapshot.Config.Dashboard.Background)
}

var errBackgroundTooLarge = errors.New("background image exceeds the size limit")

func readBackground(path string) ([]byte, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return nil, time.Time{}, errors.New("background is not a regular file")
	}
	if info.Size() > maxBackgroundBytes {
		return nil, time.Time{}, errBackgroundTooLarge
	}
	body, err := os.ReadFile(path) //nolint:gosec // G304: operator-configured path from dashboard.background, not request input; sniffed before anything is served
	if err != nil {
		return nil, time.Time{}, err
	}
	return body, info.ModTime(), nil
}
