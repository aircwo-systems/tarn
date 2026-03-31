package api

import (
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:ui-dist
var uiFS embed.FS

const htmlToServe = "200.html"

// Define a package-level boot time for the http.ServeContent modtime
var bootTime = time.Now()

func (s *Server) registerUIRoutes(mux *http.ServeMux) {
	if !s.cfg.UIEnabled {
		return
	}

	strippedFS, err := fs.Sub(uiFS, "ui-dist")
	if err != nil {
		log.Printf("[ui] failed to locate embedded assets: %v", err)
		return
	}

	handler := newUIHandler(strippedFS)
	s.ui = handler
	mux.Handle("/", handler)
	mux.Handle("GET /favicon.svg", handler)
	// Explicitly register the SvelteKit asset prefix so it takes priority over
	// the S3 wildcard route GET /{bucket}/{key...}, which would otherwise match
	// /_app/immutable/... and return 404 (no bucket named "_app").
	mux.Handle("GET /_app/", handler)
}

func newUIHandler(static fs.FS) http.Handler {
	log.Printf("[ui] serving dashboard assets from embedded filesystem")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API Protection
		apiPrefixes := []string{"/2015-03-31/", "/_tarn/", "/v2", "/_apigateway"}
		for _, prefix := range apiPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				http.NotFound(w, r)
				return
			}
		}

		cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleanPath == "." || cleanPath == "" {
			cleanPath = htmlToServe
		}

		// Try to open the requested file
		file, err := static.Open(cleanPath)
		if err != nil {
			// Fallback to 200.html for SPA routing
			file, err = static.Open(htmlToServe)
			if err != nil {
				http.Error(w, "UI "+htmlToServe+" not found", http.StatusNotFound)
				return
			}
			cleanPath = htmlToServe
		}
		defer file.Close()

		// Type assertion to ReadSeeker for http.ServeContent
		// Files from embed.FS implement io.ReadSeeker automatically
		seeker, ok := file.(io.ReadSeeker)
		if !ok {
			http.Error(w, "File does not support seeking", http.StatusInternalServerError)
			return
		}

		_, _ = file.Stat()
		http.ServeContent(w, r, cleanPath, bootTime, seeker)
	})
}
