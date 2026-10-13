//go:build !lite

package api

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
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
	mux.Handle("GET /favicon.ico", handler)
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
		if cleanPath == "favicon.ico" {
			cleanPath = "favicon.svg"
		}

		// Try to open the requested file
		file, gzipped, err := openUIAsset(static, cleanPath)
		if err != nil {
			// Fallback to 200.html for SPA routing
			file, gzipped, err = openUIAsset(static, htmlToServe)
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

		// Hashed build output never changes under the same name; everything
		// else (the SPA shell, favicon) must revalidate so a new build shows up.
		if strings.HasPrefix(cleanPath, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		if !gzipped {
			http.ServeContent(w, r, cleanPath, bootTime, seeker)
			return
		}

		// The build stores text assets gzipped (scripts/compress-ui.sh). Send
		// them as-is to clients that accept gzip, and inflate for the rest.
		w.Header().Set("Vary", "Accept-Encoding")
		if ctype := mime.TypeByExtension(path.Ext(cleanPath)); ctype != "" {
			w.Header().Set("Content-Type", ctype)
		}
		if acceptsGzip(r.Header.Get("Accept-Encoding")) {
			w.Header().Set("Content-Encoding", "gzip")
			http.ServeContent(w, r, cleanPath, bootTime, seeker)
			return
		}
		zr, err := gzip.NewReader(seeker)
		if err != nil {
			http.Error(w, "UI asset is corrupt", http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(zr)
		if err != nil {
			http.Error(w, "UI asset is corrupt", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, cleanPath, bootTime, bytes.NewReader(body))
	})
}

// openUIAsset opens name, or the gzipped copy the build left in its place.
// gzipped reports which one it found.
func openUIAsset(static fs.FS, name string) (file fs.File, gzipped bool, err error) {
	if file, err = static.Open(name); err == nil {
		return file, false, nil
	}
	if file, err = static.Open(name + ".gz"); err == nil {
		return file, true, nil
	}
	return nil, false, err
}

// acceptsGzip reports whether an Accept-Encoding header allows gzip, either
// by name or through "*", without a zero q-value.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		coding = strings.TrimSpace(coding)
		if !strings.EqualFold(coding, "gzip") && coding != "*" {
			continue
		}
		q, found := strings.CutPrefix(strings.TrimSpace(params), "q=")
		if !found {
			return true
		}
		weight, err := strconv.ParseFloat(strings.TrimSpace(q), 64)
		return err == nil && weight > 0
	}
	return false
}
