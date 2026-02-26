package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/openstack-project/openstack/internal/config"
	lambdahandler "github.com/openstack-project/openstack/internal/api/lambda"
	lambdasvc "github.com/openstack-project/openstack/internal/lambda"
)

// Server is the main OpenStack API server.
type Server struct {
	cfg        *config.Config
	httpServer *http.Server
	lambda     *lambdahandler.Handler
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, lambdaSvc *lambdasvc.Service) *Server {
	s := &Server{
		cfg:    cfg,
		lambda: lambdahandler.NewHandler(lambdaSvc),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      withLogging(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // Lambda can run up to 15 min
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /_openstack/health", s.healthHandler)

	// Lambda API — AWS-compatible endpoints
	mux.HandleFunc("POST /2015-03-31/functions", s.lambda.CreateFunction)
	mux.HandleFunc("GET /2015-03-31/functions", s.lambda.ListFunctions)
	mux.HandleFunc("GET /2015-03-31/functions/{name}", s.lambda.GetFunction)
	mux.HandleFunc("DELETE /2015-03-31/functions/{name}", s.lambda.DeleteFunction)
	mux.HandleFunc("PUT /2015-03-31/functions/{name}/code", s.lambda.UpdateFunctionCode)
	mux.HandleFunc("POST /2015-03-31/functions/{name}/invocations", s.lambda.Invoke)
}

// Start begins listening for requests.
func (s *Server) Start() error {
	log.Printf("OpenStack API server listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"running","services":["lambda"]}`)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(wrapped, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, wrapped.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
