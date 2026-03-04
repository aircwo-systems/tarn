package api

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) registerUIRoutes(mux *http.ServeMux) {
	if !s.cfg.UIEnabled {
		return
	}

	uiHandler := newUIHandler(s.cfg.UIDir)
	// Register only a single catch-all route to avoid pattern conflicts.
	mux.Handle("/", uiHandler)
}

func newUIHandler(dir string) http.Handler {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		log.Printf("[ui] dashboard enabled but assets directory not found: %s", dir)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"dashboard assets not found; build ui and set OPENSTACK_UI_DIR or --ui-dir"}`))
		})
	}

	log.Printf("[ui] serving dashboard assets from %s", dir)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Preserve API behavior for unhandled API-like paths.
		if strings.HasPrefix(r.URL.Path, "/2015-03-31/") {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_openstack/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/v2" || strings.HasPrefix(r.URL.Path, "/v2/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/_apigateway" || strings.HasPrefix(r.URL.Path, "/_apigateway/") {
			http.NotFound(w, r)
			return
		}

		clean := path.Clean("/" + r.URL.Path)
		rel := strings.TrimPrefix(clean, "/")
		if rel == "" {
			serveExistingOrIndex(w, r, dir, "200.html")
			return
		}

		serveExistingOrIndex(w, r, dir, rel)
	})
}

func serveExistingOrIndex(w http.ResponseWriter, r *http.Request, dir, rel string) {
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if fileExists(target) {
		http.ServeFile(w, r, target)
		return
	}
	http.ServeFile(w, r, filepath.Join(dir, "index.html"))
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
